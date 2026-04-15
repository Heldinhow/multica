ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_mode_check;
ALTER TABLE workflow_run DROP COLUMN IF EXISTS run_mode;
