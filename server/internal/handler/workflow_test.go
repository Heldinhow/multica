package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkflowLifecycle_CreatePlanApprove(t *testing.T) {
	// Create a regular issue first.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Workflow test issue",
		"status":   "todo",
		"priority": "high",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	// Start the workflow.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/workflows", nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var run WorkflowRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode workflow run: %v", err)
	}
	if run.Status != "planning" {
		t.Fatalf("CreateWorkflow: expected planning status, got %q", run.Status)
	}
	if len(run.Steps) != 1 || run.Steps[0].Role != "planner" {
		t.Fatalf("CreateWorkflow: expected a single planner step, got %+v", run.Steps)
	}

	// Simulate planner completion by feeding a valid JSON plan into the workflow service.
	tasks, err := testHandler.Queries.ListTasksByIssue(req.Context(), parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected planner task to be created")
	}
	var plannerTask db.AgentTaskQueue
	for _, task := range tasks {
		if task.WorkflowStepID.Valid {
			plannerTask = task
			break
		}
	}
	if !plannerTask.ID.Valid {
		t.Fatal("expected planner task with workflow_step_id")
	}
	if _, err := testPool.Exec(req.Context(), `
		UPDATE agent_task_queue
		SET status = 'completed', started_at = now(), completed_at = now()
		WHERE id = $1
	`, plannerTask.ID); err != nil {
		t.Fatalf("mark planner task completed: %v", err)
	}

	planJSON := `{
	  "plan_summary": "Implement change and verify it safely.",
	  "steps": [
	    {
	      "id": "coder-step",
	      "role": "coder",
	      "title": "Implement the change",
	      "objective": "Modify the repository and produce a diff summary.",
	      "required_capabilities": [],
	      "repo_targets": ["github.com/multica-ai/multica"],
	      "write_scope": "repo_write",
	      "expected_artifacts": ["diff_summary"],
	      "approval_gate": true
	    },
	    {
	      "id": "tester-step",
	      "role": "tester",
	      "title": "Validate the result",
	      "objective": "Run the relevant tests and summarize the outcome.",
	      "required_capabilities": [],
	      "repo_targets": ["github.com/multica-ai/multica"],
	      "write_scope": "read_only",
	      "expected_artifacts": ["test_report"],
	      "approval_gate": true
	    }
	  ],
	  "deps": [
	    { "from": "coder-step", "to": "tester-step" }
	  ]
	}`
	if err := testHandler.WorkflowService.HandleTaskCompletion(req.Context(), plannerTask, planJSON); err != nil {
		t.Fatalf("HandleTaskCompletion(planner): %v", err)
	}

	runModel, err := testHandler.Queries.GetWorkflowRun(req.Context(), parseUUID(run.ID))
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if runModel.Status != "awaiting_plan_approval" {
		t.Fatalf("planner completion: expected awaiting_plan_approval, got %q", runModel.Status)
	}

	approvals, err := testHandler.Queries.ListWorkflowApprovalsByRun(req.Context(), runModel.ID)
	if err != nil {
		t.Fatalf("ListWorkflowApprovalsByRun: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Scope != "plan" || approvals[0].Status != "pending" {
		t.Fatalf("expected one pending plan approval, got %+v", approvals)
	}

	// Approve the generated plan through the public handler.
	w = httptest.NewRecorder()
	runID := uuidToString(runModel.ID)
	req = newRequest("POST", "/api/workflows/"+runID+"/approvals/"+uuidToString(approvals[0].ID)+"/approve?workspace_id="+testWorkspaceID, map[string]any{
		"comment": "Looks good",
	})
	req = withURLParam(req, "runId", runID)
	req = withURLParam(req, "approvalId", uuidToString(approvals[0].ID))
	testHandler.ApproveWorkflowApproval(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ApproveWorkflowApproval: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var approved WorkflowRunResponse
	if err := json.NewDecoder(w.Body).Decode(&approved); err != nil {
		t.Fatalf("decode approved workflow: %v", err)
	}
	if approved.Status != "executing" {
		t.Fatalf("ApproveWorkflowApproval: expected executing status, got %q", approved.Status)
	}
	coderQueued := false
	for _, step := range approved.Steps {
		if step.Role == "coder" && step.Status == "queued" {
			coderQueued = true
		}
	}
	if !coderQueued {
		t.Fatalf("expected coder step to be queued after approval, got %+v", approved.Steps)
	}
}

func TestCreateWorkflow_CancelsPendingAssignmentTask(t *testing.T) {
	var agentID string
	if err := testPool.QueryRow(t.Context(), `
		SELECT id
		FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Workflow assigned issue",
		"status":        "todo",
		"priority":      "high",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	tasks, err := testHandler.Queries.ListTasksByIssue(req.Context(), parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one pending assignment task, got %d", len(tasks))
	}
	if tasks[0].Status != "queued" {
		t.Fatalf("expected initial task to be queued, got %q", tasks[0].Status)
	}
	if tasks[0].WorkflowStepID.Valid {
		t.Fatal("expected initial task to be a non-workflow assignment task")
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/workflows", nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	tasks, err = testHandler.Queries.ListTasksByIssue(req.Context(), parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListTasksByIssue after workflow create: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected cancelled assignment task plus planner task, got %d tasks", len(tasks))
	}

	cancelledFound := false
	plannerQueuedFound := false
	for _, task := range tasks {
		if !task.WorkflowStepID.Valid && task.Status == "cancelled" {
			cancelledFound = true
		}
		if task.WorkflowStepID.Valid && task.Status == "queued" {
			plannerQueuedFound = true
		}
	}
	if !cancelledFound {
		t.Fatal("expected original assignment task to be cancelled when workflow starts")
	}
	if !plannerQueuedFound {
		t.Fatal("expected planner workflow task to be queued")
	}
}

func TestCreateWorkflow(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Workflow run mode test issue",
		"status":   "todo",
		"priority": "high",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/workflows?workspace_id="+testWorkspaceID, map[string]any{
		"run_mode": "planning_only",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var run WorkflowRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode workflow run: %v", err)
	}
	if run.RunMode != "planning_only" {
		t.Fatalf("CreateWorkflow: expected run_mode planning_only, got %q", run.RunMode)
	}
}
