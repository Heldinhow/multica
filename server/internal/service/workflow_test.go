package service

import (
	"slices"
	"testing"
)

// TestAllowedTransitions_TargetsAreKnownStages verifies that every target in the
// allowedTransitions map refers to a stage that itself exists in the map.
func TestAllowedTransitions_TargetsAreKnownStages(t *testing.T) {
	for from, targets := range allowedTransitions {
		for _, to := range targets {
			if _, ok := allowedTransitions[to]; !ok {
				t.Errorf("allowedTransitions[%q] → %q: target stage does not exist in map", from, to)
			}
		}
	}
}

// TestStageToStatus_CoversAllAllowedStages verifies that every stage in
// allowedTransitions has a corresponding entry in stageToStatus.
func TestStageToStatus_CoversAllAllowedStages(t *testing.T) {
	for stage := range allowedTransitions {
		if _, ok := stageToStatus[stage]; !ok {
			t.Errorf("stageToStatus missing entry for stage %q (present in allowedTransitions)", stage)
		}
	}
}

// TestAllowedTransitions_DoneIsTerminal verifies that "done" has no outgoing
// transitions (it is the terminal state).
func TestAllowedTransitions_DoneIsTerminal(t *testing.T) {
	targets, ok := allowedTransitions["done"]
	if !ok {
		t.Fatal("allowedTransitions missing 'done' stage")
	}
	if len(targets) != 0 {
		t.Errorf("expected 'done' to have no transitions, got %v", targets)
	}
}

// TestAllowedTransitions_PlanApprovalReachableFromArtifactReview verifies the
// critical SDD gate: artifact_review can advance to hitl_plan_approval.
func TestAllowedTransitions_PlanApprovalReachableFromArtifactReview(t *testing.T) {
	targets := allowedTransitions["artifact_review"]
	if !slices.Contains(targets, "hitl_plan_approval") {
		t.Errorf("artifact_review should allow transition to hitl_plan_approval, got %v", targets)
	}
}

// TestAllowedTransitions_ArtifactReviewCanRollback verifies that artifact_review
// supports rollback to planning stages.
func TestAllowedTransitions_ArtifactReviewCanRollback(t *testing.T) {
	targets := allowedTransitions["artifact_review"]
	for _, rollback := range []string{"planning_plan", "planning_clarify"} {
		if !slices.Contains(targets, rollback) {
			t.Errorf("artifact_review should allow rollback to %q, got %v", rollback, targets)
		}
	}
}

// TestAllowedTransitions_ExecutionGateAfterHITL verifies that execution only
// starts from execution_ready, not directly from hitl_plan_approval.
func TestAllowedTransitions_ExecutionGateAfterHITL(t *testing.T) {
	hitlTargets := allowedTransitions["hitl_plan_approval"]
	if slices.Contains(hitlTargets, "execution") {
		t.Error("hitl_plan_approval must not transition directly to execution; use execution_ready as gate")
	}
	if !slices.Contains(hitlTargets, "execution_ready") {
		t.Errorf("hitl_plan_approval must allow transition to execution_ready, got %v", hitlTargets)
	}
	readyTargets := allowedTransitions["execution_ready"]
	if !slices.Contains(readyTargets, "execution") {
		t.Errorf("execution_ready must allow transition to execution, got %v", readyTargets)
	}
}

// TestStageRejectionTarget_KnownStages verifies all rejection targets are valid stages.
func TestStageRejectionTarget_KnownStages(t *testing.T) {
	for stage, target := range stageRejectionTarget {
		if _, ok := allowedTransitions[stage]; !ok {
			t.Errorf("stageRejectionTarget[%q]: source stage not in allowedTransitions", stage)
		}
		if _, ok := allowedTransitions[target]; !ok {
			t.Errorf("stageRejectionTarget[%q] = %q: target stage not in allowedTransitions", stage, target)
		}
	}
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
