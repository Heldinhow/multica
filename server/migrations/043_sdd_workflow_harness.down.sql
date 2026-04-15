-- Revert workflow_approval scope
ALTER TABLE workflow_approval
    DROP CONSTRAINT workflow_approval_scope_check;
ALTER TABLE workflow_approval
    ADD CONSTRAINT workflow_approval_scope_check CHECK (scope IN (
        'plan',
        'handoff',
        'finalize'
    ));

-- Revert workflow_artifact types
ALTER TABLE workflow_artifact
    DROP CONSTRAINT workflow_artifact_artifact_type_check;
ALTER TABLE workflow_artifact
    ADD CONSTRAINT workflow_artifact_artifact_type_check CHECK (artifact_type IN (
        'plan',
        'handoff_note',
        'diff_summary',
        'review_report',
        'test_report',
        'final_summary'
    ));

-- Revert workflow_step roles
ALTER TABLE workflow_step
    DROP CONSTRAINT workflow_step_role_check;
ALTER TABLE workflow_step
    ADD CONSTRAINT workflow_step_role_check CHECK (role IN (
        'planner',
        'coder',
        'reviewer',
        'tester'
    ));

-- Revert workflow_run status
ALTER TABLE workflow_run
    DROP CONSTRAINT workflow_run_status_check;
ALTER TABLE workflow_run
    ADD CONSTRAINT workflow_run_status_check CHECK (status IN (
        'planning',
        'awaiting_plan_approval',
        'executing',
        'awaiting_handoff_approval',
        'blocked',
        'completed',
        'cancelled'
    ));

-- Remove added columns
ALTER TABLE workflow_run
    DROP COLUMN IF EXISTS current_stage,
    DROP COLUMN IF EXISTS run_mode;
