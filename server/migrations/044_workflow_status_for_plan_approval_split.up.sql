ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_status_check;
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_status_check CHECK (status IN (
    'planning',
    'awaiting_plan_approval',
    'planning_complete',
    'execution_ready',
    'executing',
    'awaiting_handoff_approval',
    'blocked',
    'completed',
    'cancelled'
));
