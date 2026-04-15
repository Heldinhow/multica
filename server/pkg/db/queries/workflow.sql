-- name: CreateWorkflowRun :one
INSERT INTO workflow_run (
    workspace_id, issue_id, status, phase, plan_version, token_budget, base_branch,
    created_by, max_steps, max_replans, max_retries_per_step, run_mode
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetWorkflowRun :one
SELECT * FROM workflow_run
WHERE id = $1;

-- name: ListWorkflowRunsByIssue :many
SELECT * FROM workflow_run
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: GetActiveWorkflowRunByIssue :one
SELECT * FROM workflow_run
WHERE issue_id = $1
  AND status IN ('planning', 'planning_complete', 'awaiting_plan_approval', 'executing', 'execution_ready', 'awaiting_handoff_approval', 'blocked')
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateWorkflowRunState :one
UPDATE workflow_run
SET
    status = COALESCE(sqlc.narg('status'), status),
    phase = COALESCE(sqlc.narg('phase'), phase),
    plan_version = COALESCE(sqlc.narg('plan_version'), plan_version),
    approved_plan_at = COALESCE(sqlc.narg('approved_plan_at'), approved_plan_at),
    cancelled_at = COALESCE(sqlc.narg('cancelled_at'), cancelled_at),
    replan_count = COALESCE(sqlc.narg('replan_count'), replan_count),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowStep :one
INSERT INTO workflow_step (
    workflow_run_id, role, title, objective, status, write_scope, repo_target,
    assigned_agent_id, requires_approval, context_version, retry_count, attempt_count,
    sort_order, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    sqlc.narg(assigned_agent_id), $8, $9, $10, $11,
    $12, $13
)
RETURNING *;

-- name: GetWorkflowStep :one
SELECT * FROM workflow_step
WHERE id = $1;

-- name: ListWorkflowStepsByRun :many
SELECT * FROM workflow_step
WHERE workflow_run_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: UpdateWorkflowStepState :one
UPDATE workflow_step
SET
    status = COALESCE(sqlc.narg('status'), status),
    assigned_agent_id = COALESCE(sqlc.narg('assigned_agent_id'), assigned_agent_id),
    requires_approval = COALESCE(sqlc.narg('requires_approval'), requires_approval),
    context_version = COALESCE(sqlc.narg('context_version'), context_version),
    retry_count = COALESCE(sqlc.narg('retry_count'), retry_count),
    attempt_count = COALESCE(sqlc.narg('attempt_count'), attempt_count),
    metadata = COALESCE(sqlc.narg('metadata'), metadata),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowStepEdge :one
INSERT INTO workflow_step_edge (workflow_run_id, from_step_id, to_step_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListWorkflowStepEdgesByRun :many
SELECT * FROM workflow_step_edge
WHERE workflow_run_id = $1
ORDER BY created_at ASC;

-- name: CreateWorkflowArtifact :one
INSERT INTO workflow_artifact (
    workflow_run_id, workflow_step_id, artifact_type, summary, content, created_by_agent_id
) VALUES (
    $1, sqlc.narg(workflow_step_id), $2, $3, $4, sqlc.narg(created_by_agent_id)
)
RETURNING *;

-- name: ListWorkflowArtifactsByRun :many
SELECT * FROM workflow_artifact
WHERE workflow_run_id = $1
ORDER BY created_at ASC;

-- name: CreateWorkflowApproval :one
INSERT INTO workflow_approval (
    workflow_run_id, workflow_step_id, from_step_id, to_step_id, scope, status, comment
) VALUES (
    $1, sqlc.narg(workflow_step_id), sqlc.narg(from_step_id), sqlc.narg(to_step_id), $2, $3, $4
)
RETURNING *;

-- name: GetWorkflowApproval :one
SELECT * FROM workflow_approval
WHERE id = $1;

-- name: ListWorkflowApprovalsByRun :many
SELECT * FROM workflow_approval
WHERE workflow_run_id = $1
ORDER BY created_at ASC;

-- name: ResolveWorkflowApproval :one
UPDATE workflow_approval
SET
    status = $2,
    reviewer_id = sqlc.narg(reviewer_id),
    comment = COALESCE(sqlc.narg('comment'), comment),
    resolved_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowEvent :one
INSERT INTO workflow_event (
    workflow_run_id, workflow_step_id, event_type, payload
) VALUES (
    $1, sqlc.narg(workflow_step_id), $2, $3
)
RETURNING *;

-- name: ListWorkflowEventsByRun :many
SELECT * FROM workflow_event
WHERE workflow_run_id = $1
ORDER BY created_at ASC;
