ALTER TABLE workflow_run ADD COLUMN run_mode TEXT NOT NULL DEFAULT 'planning_plus_execution';
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_mode_check CHECK (run_mode IN ('planning_only', 'planning_plus_execution'));
