DROP INDEX IF EXISTS idx_agent_task_queue_workflow_step;
DROP INDEX IF EXISTS idx_workflow_event_run;
DROP INDEX IF EXISTS idx_workflow_approval_run;
DROP INDEX IF EXISTS idx_workflow_artifact_run;
DROP INDEX IF EXISTS idx_workflow_edge_run;
DROP INDEX IF EXISTS idx_workflow_step_status;
DROP INDEX IF EXISTS idx_workflow_step_run;
DROP INDEX IF EXISTS idx_workflow_run_workspace;
DROP INDEX IF EXISTS idx_workflow_run_issue;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS attempt_no,
    DROP COLUMN IF EXISTS workflow_step_id;

DROP TABLE IF EXISTS workflow_event;
DROP TABLE IF EXISTS workflow_approval;
DROP TABLE IF EXISTS workflow_artifact;
DROP TABLE IF EXISTS workflow_step_edge;
DROP TABLE IF EXISTS workflow_step;
DROP TABLE IF EXISTS workflow_run;

ALTER TABLE agent
    DROP COLUMN IF EXISTS tool_policy,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS workflow_roles;
