package service

import "testing"

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
