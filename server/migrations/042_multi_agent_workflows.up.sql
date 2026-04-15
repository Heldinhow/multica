ALTER TABLE agent
    ADD COLUMN workflow_roles TEXT[] NOT NULL DEFAULT ARRAY['planner', 'coder', 'reviewer', 'tester'],
    ADD COLUMN capabilities JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN tool_policy JSONB NOT NULL DEFAULT '{}';

CREATE TABLE workflow_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'planning'
        CHECK (status IN (
            'planning',
            'awaiting_plan_approval',
            'executing',
            'awaiting_handoff_approval',
            'blocked',
            'completed',
            'cancelled'
        )),
    phase TEXT NOT NULL DEFAULT 'planning',
    plan_version INT NOT NULL DEFAULT 1,
    token_budget BIGINT NOT NULL DEFAULT 0,
    base_branch TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    approved_plan_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    max_steps INT NOT NULL DEFAULT 20,
    max_replans INT NOT NULL DEFAULT 2,
    max_retries_per_step INT NOT NULL DEFAULT 2,
    replan_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_step (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('planner', 'coder', 'reviewer', 'tester')),
    title TEXT NOT NULL,
    objective TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN (
            'draft',
            'ready',
            'queued',
            'leased',
            'running',
            'awaiting_approval',
            'completed',
            'failed',
            'blocked',
            'cancelled',
            'rejected'
        )),
    write_scope TEXT NOT NULL DEFAULT 'read_only'
        CHECK (write_scope IN ('none', 'read_only', 'repo_write')),
    repo_target TEXT NOT NULL DEFAULT '',
    assigned_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    context_version INT NOT NULL DEFAULT 1,
    retry_count INT NOT NULL DEFAULT 0,
    attempt_count INT NOT NULL DEFAULT 0,
    sort_order INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_step_edge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    from_step_id UUID NOT NULL REFERENCES workflow_step(id) ON DELETE CASCADE,
    to_step_id UUID NOT NULL REFERENCES workflow_step(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_run_id, from_step_id, to_step_id)
);

CREATE TABLE workflow_artifact (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    workflow_step_id UUID REFERENCES workflow_step(id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL CHECK (artifact_type IN (
        'plan',
        'handoff_note',
        'diff_summary',
        'review_report',
        'test_report',
        'final_summary'
    )),
    summary TEXT NOT NULL DEFAULT '',
    content JSONB NOT NULL DEFAULT '{}',
    created_by_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_approval (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    workflow_step_id UUID REFERENCES workflow_step(id) ON DELETE CASCADE,
    from_step_id UUID REFERENCES workflow_step(id) ON DELETE CASCADE,
    to_step_id UUID REFERENCES workflow_step(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('plan', 'handoff', 'finalize')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewer_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE TABLE workflow_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    workflow_step_id UUID REFERENCES workflow_step(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue
    ADD COLUMN workflow_step_id UUID REFERENCES workflow_step(id) ON DELETE SET NULL,
    ADD COLUMN attempt_no INT NOT NULL DEFAULT 1;

CREATE INDEX idx_workflow_run_issue ON workflow_run(issue_id, created_at DESC);
CREATE INDEX idx_workflow_run_workspace ON workflow_run(workspace_id, created_at DESC);
CREATE INDEX idx_workflow_step_run ON workflow_step(workflow_run_id, sort_order, created_at);
CREATE INDEX idx_workflow_step_status ON workflow_step(workflow_run_id, status);
CREATE INDEX idx_workflow_edge_run ON workflow_step_edge(workflow_run_id);
CREATE INDEX idx_workflow_artifact_run ON workflow_artifact(workflow_run_id, created_at DESC);
CREATE INDEX idx_workflow_approval_run ON workflow_approval(workflow_run_id, status, created_at DESC);
CREATE INDEX idx_workflow_event_run ON workflow_event(workflow_run_id, created_at DESC);
CREATE INDEX idx_agent_task_queue_workflow_step ON agent_task_queue(workflow_step_id, status);
