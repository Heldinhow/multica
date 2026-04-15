export interface WorkflowTaskAttempt {
  id: string;
  workflow_step_id?: string | null;
  status: string;
  attempt_no: number;
  agent_id: string;
  runtime_id: string;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  error: string | null;
}

export interface WorkflowStep {
  id: string;
  workflow_run_id: string;
  role: "planner" | "coder" | "reviewer" | "tester" | "artifact_reviewer" | "code_reviewer" | "pr_creator";
  title: string;
  objective: string;
  status:
    | "draft"
    | "ready"
    | "queued"
    | "leased"
    | "running"
    | "awaiting_approval"
    | "completed"
    | "failed"
    | "blocked"
    | "cancelled"
    | "rejected";
  write_scope: "none" | "read_only" | "repo_write";
  repo_target: string;
  assigned_agent_id: string | null;
  requires_approval: boolean;
  context_version: number;
  retry_count: number;
  attempt_count: number;
  sort_order: number;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  attempts: WorkflowTaskAttempt[];
}

export interface WorkflowEdge {
  id: string;
  workflow_run_id: string;
  from_step_id: string;
  to_step_id: string;
  created_at: string;
}

export interface WorkflowArtifact {
  id: string;
  workflow_run_id: string;
  workflow_step_id: string | null;
  artifact_type:
    | "plan"
    | "handoff_note"
    | "diff_summary"
    | "review_report"
    | "test_report"
    | "final_summary"
    | "spec"
    | "tasks"
    | "artifact_review_report"
    | "code_review_report"
    | "workflow_state";
  summary: string;
  content: Record<string, unknown>;
  created_by_agent_id: string | null;
  created_at: string;
}

export interface WorkflowApproval {
  id: string;
  workflow_run_id: string;
  workflow_step_id: string | null;
  from_step_id: string | null;
  to_step_id: string | null;
  scope: "plan" | "handoff" | "finalize" | "execution_start";
  status: "pending" | "approved" | "rejected";
  reviewer_id: string | null;
  comment: string;
  created_at: string;
  resolved_at: string | null;
}

export interface WorkflowEvent {
  id: string;
  workflow_run_id: string;
  workflow_step_id: string | null;
  event_type: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface WorkflowRun {
  id: string;
  workspace_id: string;
  issue_id: string;
  status:
    | "planning"
    | "in_artifact_review"
    | "awaiting_plan_approval"
    | "execution_ready"
    | "executing"
    | "awaiting_handoff_approval"
    | "in_code_review"
    | "in_pr_creation"
    | "blocked"
    | "completed"
    | "cancelled";
  run_mode: "planning_only" | "planning_plus_execution";
  current_stage: string;
  phase: string;
  plan_version: number;
  token_budget: number;
  base_branch: string;
  created_by: string;
  approved_plan_at: string | null;
  cancelled_at: string | null;
  max_steps: number;
  max_replans: number;
  max_retries_per_step: number;
  replan_count: number;
  created_at: string;
  updated_at: string;
  steps: WorkflowStep[];
  edges: WorkflowEdge[];
  artifacts: WorkflowArtifact[];
  approvals: WorkflowApproval[];
  events: WorkflowEvent[];
}
