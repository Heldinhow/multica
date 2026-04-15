-- 1. run_mode: separates planning-only from execution mode
ALTER TABLE workflow_run
    ADD COLUMN run_mode TEXT NOT NULL DEFAULT 'planning_only'
        CHECK (run_mode IN ('planning_only', 'planning_plus_execution'));

-- 2. current_stage: granular SDD stage tracker (phase = macro-phase, current_stage = SDD stage)
ALTER TABLE workflow_run
    ADD COLUMN current_stage TEXT NOT NULL DEFAULT 'intake';

-- 3. Expand workflow_run status to include SDD operational states
ALTER TABLE workflow_run
    DROP CONSTRAINT workflow_run_status_check;
ALTER TABLE workflow_run
    ADD CONSTRAINT workflow_run_status_check CHECK (status IN (
        'planning',
        'in_artifact_review',
        'awaiting_plan_approval',
        'execution_ready',
        'executing',
        'awaiting_handoff_approval',
        'in_code_review',
        'in_pr_creation',
        'blocked',
        'completed',
        'cancelled'
    ));

-- 4. Expand workflow_step roles for SDD reviewer/creator agents
ALTER TABLE workflow_step
    DROP CONSTRAINT workflow_step_role_check;
ALTER TABLE workflow_step
    ADD CONSTRAINT workflow_step_role_check CHECK (role IN (
        'planner',
        'coder',
        'reviewer',
        'tester',
        'artifact_reviewer',
        'code_reviewer',
        'pr_creator'
    ));

-- 5. Expand workflow_artifact types for SDD artifacts
ALTER TABLE workflow_artifact
    DROP CONSTRAINT workflow_artifact_artifact_type_check;
ALTER TABLE workflow_artifact
    ADD CONSTRAINT workflow_artifact_artifact_type_check CHECK (artifact_type IN (
        'plan',
        'handoff_note',
        'diff_summary',
        'review_report',
        'test_report',
        'final_summary',
        'spec',
        'tasks',
        'artifact_review_report',
        'code_review_report',
        'workflow_state'
    ));

-- 6. Expand workflow_approval scope for explicit execution-start gate
ALTER TABLE workflow_approval
    DROP CONSTRAINT workflow_approval_scope_check;
ALTER TABLE workflow_approval
    ADD CONSTRAINT workflow_approval_scope_check CHECK (scope IN (
        'plan',
        'handoff',
        'finalize',
        'execution_start'
    ));
