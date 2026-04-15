package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestApprove_WrongStatus_ReturnsError(t *testing.T) {
	svc := &WorkflowService{}

	run := db.WorkflowRun{
		ID:      mustParseUUID("11111111-1111-1111-1111-111111111111"),
		Status:  "planning",
		RunMode: "planning_plus_execution",
	}
	approval := db.WorkflowApproval{
		ID:    mustParseUUID("22222222-2222-2222-2222-222222222222"),
		Scope: "plan",
	}
	reviewer := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}

	err := svc.Approve(context.Background(), run, approval, reviewer, "approved")
	if err == nil {
		t.Fatal("expected error when run is not in awaiting_plan_approval status")
	}
	if err.Error() != "workflow run must be in awaiting_plan_approval status to approve" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestStartExecution_WrongRunMode_ReturnsError(t *testing.T) {
	svc := &WorkflowService{}

	run := db.WorkflowRun{
		ID:      mustParseUUID("11111111-1111-1111-1111-111111111111"),
		Status:  "execution_ready",
		RunMode: "planning_only",
	}

	err := svc.StartExecution(context.Background(), run)
	if err == nil {
		t.Fatal("expected error when run mode is not planning_plus_execution")
	}
	if err.Error() != "StartExecution requires planning_plus_execution mode, got: planning_only" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestStartExecution_WrongStatus_ReturnsError(t *testing.T) {
	svc := &WorkflowService{}

	run := db.WorkflowRun{
		ID:      mustParseUUID("11111111-1111-1111-1111-111111111111"),
		Status:  "awaiting_plan_approval",
		RunMode: "planning_plus_execution",
	}

	err := svc.StartExecution(context.Background(), run)
	if err == nil {
		t.Fatal("expected error when run is not in execution_ready status")
	}
	if err.Error() != "workflow run must be in execution_ready status to start execution, got: awaiting_plan_approval" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func mustParseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func TestParsePlannerOutput_AcceptsPlainJSON(t *testing.T) {
	output := `{"plan_summary":"test","steps":[{"id":"step-1","role":"coder","title":"Do work","objective":"Implement","required_capabilities":[],"repo_targets":["https://github.com/Heldinhow/multica"],"write_scope":"repo_write","expected_artifacts":["diff_summary"],"approval_gate":true}],"deps":[]}`

	plan, err := parsePlannerOutput(output)
	if err != nil {
		t.Fatalf("parsePlannerOutput returned error: %v", err)
	}
	if plan.PlanSummary != "test" {
		t.Fatalf("expected plan_summary to be preserved, got %q", plan.PlanSummary)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
}

func TestParsePlannerOutput_AcceptsFencedJSONWithExtraText(t *testing.T) {
	output := "Here is the workflow plan.\n\n```json\n{\"plan_summary\":\"test\",\"steps\":[{\"id\":\"step-1\",\"role\":\"coder\",\"title\":\"Do work\",\"objective\":\"Implement\",\"required_capabilities\":[],\"repo_targets\":[\"https://github.com/Heldinhow/multica\"],\"write_scope\":\"repo_write\",\"expected_artifacts\":[\"diff_summary\"],\"approval_gate\":true}],\"deps\":[]}\n```\n\nI will wait for approval."

	plan, err := parsePlannerOutput(output)
	if err != nil {
		t.Fatalf("parsePlannerOutput returned error: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
}

func TestParsePlannerOutput_AcceptsEmbeddedJSONObject(t *testing.T) {
	output := "Workflow plan follows:\n{\"plan_summary\":\"test\",\"steps\":[{\"id\":\"step-1\",\"role\":\"tester\",\"title\":\"Validate\",\"objective\":\"Verify\",\"required_capabilities\":[],\"repo_targets\":[\"https://github.com/Heldinhow/multica\"],\"write_scope\":\"read_only\",\"expected_artifacts\":[\"test_report\"],\"approval_gate\":true}],\"deps\":[]}\nThanks."

	plan, err := parsePlannerOutput(output)
	if err != nil {
		t.Fatalf("parsePlannerOutput returned error: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Role != "tester" {
		t.Fatalf("expected tester role, got %q", plan.Steps[0].Role)
	}
}

func TestValidatePlannerOutput_DetectsExecutionClaim(t *testing.T) {
	output := `{"plan_summary":"test","steps":[{"id":"step-1","role":"coder","title":"Do work","objective":"Implement","required_capabilities":[],"repo_targets":["https://github.com/Heldinhow/multica"],"write_scope":"repo_write","expected_artifacts":["diff_summary"],"approval_gate":true}],"deps":[]} + I changed the issue status to in_progress`

	result := validatePlannerOutput(output)
	if !result.BlockRun {
		t.Fatal("expected BlockRun to be true when output contains execution claim")
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty reason when blocked")
	}
}

func TestValidatePlannerOutput_AcceptsCleanOutput(t *testing.T) {
	output := `{"plan_summary":"test","steps":[{"id":"step-1","role":"coder","title":"Do work","objective":"Implement","required_capabilities":[],"repo_targets":["https://github.com/Heldinhow/multica"],"write_scope":"repo_write","expected_artifacts":["diff_summary"],"approval_gate":true}],"deps":[]}`

	result := validatePlannerOutput(output)
	if result.BlockRun {
		t.Fatal("expected BlockRun to be false for clean output")
	}
	if !result.AcceptPlan {
		t.Fatal("expected AcceptPlan to be true for clean output")
	}
}

func TestValidatePlannerOutput_DetectsVariousExecutionClaims(t *testing.T) {
	forbiddenPhrases := []string{
		"i changed the issue status",
		"i updated the issue",
		"i implemented",
		"i made the changes",
		"i modified the codebase",
		"i changed the codebase",
	}

	validPlan := `{"plan_summary":"test","steps":[{"id":"step-1","role":"coder","title":"Do work","objective":"Implement","required_capabilities":[],"repo_targets":["https://github.com/Heldinhow/multica"],"write_scope":"repo_write","expected_artifacts":["diff_summary"],"approval_gate":true}],"deps":[]}`

	for _, phrase := range forbiddenPhrases {
		mixedOutput := validPlan + " " + phrase
		result := validatePlannerOutput(mixedOutput)
		if !result.BlockRun {
			t.Errorf("expected BlockRun=true for forbidden phrase %q", phrase)
		}
	}
}
