package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WorkflowService struct {
	Queries   *db.Queries
	TxStarter interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	}
	Bus *events.Bus
}

func NewWorkflowService(q *db.Queries, txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}, bus *events.Bus) *WorkflowService {
	return &WorkflowService{
		Queries:   q,
		TxStarter: txStarter,
		Bus:       bus,
	}
}

type WorkflowPlan struct {
	PlanSummary string             `json:"plan_summary"`
	BaseBranch  string             `json:"base_branch"`
	Steps       []WorkflowPlanStep `json:"steps"`
	Deps        []WorkflowPlanEdge `json:"deps"`
}

type WorkflowPlanStep struct {
	LocalID              string   `json:"id"`
	Role                 string   `json:"role"`
	Title                string   `json:"title"`
	Objective            string   `json:"objective"`
	RequiredCapabilities []string `json:"required_capabilities"`
	RepoTargets          []string `json:"repo_targets"`
	WriteScope           string   `json:"write_scope"`
	ExpectedArtifacts    []string `json:"expected_artifacts"`
	ApprovalGate         bool     `json:"approval_gate"`
}

type WorkflowPlanEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// allowedTransitions defines the valid SDD stage machine transitions.
// Any transition not listed here is rejected by TransitionStage.
var allowedTransitions = map[string][]string{
	"intake":             {"planning_specify"},
	"planning_specify":   {"planning_plan", "planning_clarify"},
	"planning_clarify":   {"planning_plan"},
	"planning_plan":      {"planning_tasks"},
	"planning_tasks":     {"artifact_review"},
	"artifact_review":    {"hitl_plan_approval", "planning_plan", "planning_clarify"},
	"hitl_plan_approval": {"execution_ready", "planning_plan", "planning_tasks"},
	"execution_ready":    {"execution"},
	"execution":          {"code_review"},
	"code_review":        {"pr_creation", "execution"},
	"pr_creation":        {"final_hitl"},
	"final_hitl":         {"done", "code_review", "execution"},
	"done":               {},
}

// stageToStatus maps the SDD stage to the corresponding workflow_run.status.
var stageToStatus = map[string]string{
	"intake":             "planning",
	"planning_specify":   "planning",
	"planning_clarify":   "planning",
	"planning_plan":      "planning",
	"planning_tasks":     "planning",
	"artifact_review":    "in_artifact_review",
	"hitl_plan_approval": "awaiting_plan_approval",
	"execution_ready":    "execution_ready",
	"execution":          "executing",
	"code_review":        "in_code_review",
	"pr_creation":        "in_pr_creation",
	"final_hitl":         "awaiting_handoff_approval",
	"done":               "completed",
}

// stageRejectionTarget maps each review/approval stage to its default rollback target.
var stageRejectionTarget = map[string]string{
	"artifact_review":    "planning_plan",
	"hitl_plan_approval": "planning_plan",
	"code_review":        "execution",
	"final_hitl":         "code_review",
}

func (s *WorkflowService) CreateRun(ctx context.Context, issue db.Issue, createdBy pgtype.UUID) (db.WorkflowRun, db.WorkflowStep, error) {
	if createdBy.Valid == false {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("created_by is required")
	}

	_, err := s.Queries.GetActiveWorkflowRunByIssue(ctx, issue.ID)
	if err == nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("an active workflow already exists for this issue")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("check active workflow: %w", err)
	}

	planner, err := s.selectAgentForRole(ctx, issue.WorkspaceID, "planner", nil, issue.AssigneeID)
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, err
	}
	if !planner.RuntimeID.Valid {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("planner agent has no runtime")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("begin workflow tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	run, err := qtx.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		WorkspaceID:       issue.WorkspaceID,
		IssueID:           issue.ID,
		Status:            "planning",
		Phase:             "planning",
		PlanVersion:       1,
		TokenBudget:       2_000_000,
		BaseBranch:        "main",
		CreatedBy:         createdBy,
		MaxSteps:          20,
		MaxReplans:        2,
		MaxRetriesPerStep: 2,
	})
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("create workflow run: %w", err)
	}

	// A workflow run is an explicit orchestration path, so it should supersede
	// any pending assignment-triggered task for the same issue before the
	// planner step is queued.
	if err := qtx.CancelAgentTasksByIssue(ctx, issue.ID); err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("cancel existing issue tasks: %w", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"required_capabilities": []string{"planning", "issue_analysis"},
		"expected_artifacts":    []string{"plan"},
		"tool_policy": map[string]any{
			"terminal_write": false,
			"repo_write":     false,
		},
	})
	step, err := qtx.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
		WorkflowRunID:    run.ID,
		Role:             "planner",
		Title:            "Plan workflow",
		Objective:        "Produce a structured execution plan for this issue using the workflow planner contract.",
		Status:           "queued",
		WriteScope:       "read_only",
		RepoTarget:       "",
		RequiresApproval: true,
		ContextVersion:   1,
		RetryCount:       0,
		AttemptCount:     1,
		SortOrder:        0,
		Metadata:         metadata,
		AssignedAgentID:  planner.ID,
	})
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("create planner step: %w", err)
	}

	if _, err := qtx.CreateWorkflowStepTask(ctx, db.CreateWorkflowStepTaskParams{
		AgentID:        planner.ID,
		RuntimeID:      planner.RuntimeID,
		IssueID:        issue.ID,
		Priority:       4,
		WorkflowStepID: step.ID,
		AttemptNo:      1,
	}); err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("create planner task: %w", err)
	}

	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID: run.ID,
		EventType:     "workflow.created",
		Payload: mustJSON(map[string]any{
			"issue_id":         util.UUIDToString(issue.ID),
			"planner_agent_id": util.UUIDToString(planner.ID),
		}),
	}); err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("create workflow event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowRun{}, db.WorkflowStep{}, fmt.Errorf("commit workflow create: %w", err)
	}

	s.publishWorkflowRun(run, "created")
	s.publishWorkflowStep(run, step, "queued")
	s.publishWorkflowEvent(run, step.ID, "workflow:event_created", map[string]any{
		"event_type": "workflow.created",
	})
	return run, step, nil
}

func (s *WorkflowService) Approve(ctx context.Context, run db.WorkflowRun, approval db.WorkflowApproval, reviewer pgtype.UUID, comment string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	if _, err := qtx.ResolveWorkflowApproval(ctx, db.ResolveWorkflowApprovalParams{
		ID:         approval.ID,
		Status:     "approved",
		ReviewerID: reviewer,
		Comment:    pgtype.Text{String: comment, Valid: comment != ""},
	}); err != nil {
		return err
	}

	switch approval.Scope {
	case "plan":
		// Plan approval moves the workflow to hitl_plan_approval stage.
		// Execution is NOT started automatically — the operator must call
		// StartExecution explicitly via POST /workflows/:runId/start-execution.
		if approval.WorkflowStepID.Valid {
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:     approval.WorkflowStepID,
				Status: pgtype.Text{String: "completed", Valid: true},
			}); err != nil {
				return err
			}
		}
		if _, err := qtx.UpdateWorkflowRunStage(ctx, db.UpdateWorkflowRunStageParams{
			ID:           run.ID,
			CurrentStage: "hitl_plan_approval",
			Status:       "awaiting_plan_approval",
		}); err != nil {
			return err
		}
		if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:             run.ID,
			ApprovedPlanAt: nowUTC(),
		}); err != nil {
			return err
		}
	case "handoff":
		if approval.WorkflowStepID.Valid {
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:     approval.WorkflowStepID,
				Status: pgtype.Text{String: "completed", Valid: true},
			}); err != nil {
				return err
			}
			if err := s.promoteDownstreamReadyStepsTx(ctx, qtx, run.ID, approval.WorkflowStepID); err != nil {
				return err
			}
		}
	case "finalize":
		if approval.WorkflowStepID.Valid {
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:     approval.WorkflowStepID,
				Status: pgtype.Text{String: "completed", Valid: true},
			}); err != nil {
				return err
			}
		}
		if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:     run.ID,
			Status: pgtype.Text{String: "completed", Valid: true},
			Phase:  pgtype.Text{String: "completed", Valid: true},
		}); err != nil {
			return err
		}
	}

	if err := s.scheduleReadyStepsTx(ctx, qtx, run.ID); err != nil {
		return err
	}
	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: approval.WorkflowStepID,
		EventType:      "approval.approved",
		Payload: mustJSON(map[string]any{
			"approval_id": util.UUIDToString(approval.ID),
			"scope":       approval.Scope,
			"comment":     comment,
		}),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowApproval(run, approval, "approved")
	s.publishWorkflowEvent(run, approval.WorkflowStepID, "workflow:event_created", map[string]any{
		"event_type": "approval.approved",
	})
	return nil
}

func (s *WorkflowService) Reject(ctx context.Context, run db.WorkflowRun, approval db.WorkflowApproval, reviewer pgtype.UUID, comment string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	if _, err := qtx.ResolveWorkflowApproval(ctx, db.ResolveWorkflowApprovalParams{
		ID:         approval.ID,
		Status:     "rejected",
		ReviewerID: reviewer,
		Comment:    pgtype.Text{String: comment, Valid: comment != ""},
	}); err != nil {
		return err
	}

	switch approval.Scope {
	case "plan":
		if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:          run.ID,
			Status:      pgtype.Text{String: "planning", Valid: true},
			Phase:       pgtype.Text{String: "replan_required", Valid: true},
			ReplanCount: pgtype.Int4{Int32: run.ReplanCount + 1, Valid: true},
		}); err != nil {
			return err
		}
		if approval.WorkflowStepID.Valid {
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:             approval.WorkflowStepID,
				Status:         pgtype.Text{String: "queued", Valid: true},
				ContextVersion: pgtype.Int4{Int32: 2, Valid: true},
				AttemptCount:   pgtype.Int4{Int32: 2, Valid: true},
			}); err != nil {
				return err
			}
			step, err := qtx.GetWorkflowStep(ctx, approval.WorkflowStepID)
			if err != nil {
				return err
			}
			agent, err := s.Queries.GetAgent(ctx, step.AssignedAgentID)
			if err != nil {
				return err
			}
			if _, err := qtx.CreateWorkflowStepTask(ctx, db.CreateWorkflowStepTaskParams{
				AgentID:        agent.ID,
				RuntimeID:      agent.RuntimeID,
				IssueID:        run.IssueID,
				Priority:       4,
				WorkflowStepID: step.ID,
				AttemptNo:      step.AttemptCount + 1,
			}); err != nil {
				return err
			}
		}
	case "handoff", "finalize":
		if approval.WorkflowStepID.Valid {
			step, err := qtx.GetWorkflowStep(ctx, approval.WorkflowStepID)
			if err != nil {
				return err
			}
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:             step.ID,
				Status:         pgtype.Text{String: "ready", Valid: true},
				ContextVersion: pgtype.Int4{Int32: step.ContextVersion + 1, Valid: true},
			}); err != nil {
				return err
			}
		}
		if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:     run.ID,
			Status: pgtype.Text{String: "executing", Valid: true},
			Phase:  pgtype.Text{String: "execution", Valid: true},
		}); err != nil {
			return err
		}
	}

	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: approval.WorkflowStepID,
		EventType:      "approval.rejected",
		Payload: mustJSON(map[string]any{
			"approval_id": util.UUIDToString(approval.ID),
			"scope":       approval.Scope,
			"comment":     comment,
		}),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowApproval(run, approval, "rejected")
	s.publishWorkflowEvent(run, approval.WorkflowStepID, "workflow:event_created", map[string]any{
		"event_type": "approval.rejected",
	})
	return nil
}

func (s *WorkflowService) Cancel(ctx context.Context, run db.WorkflowRun) error {
	updated, err := s.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:          run.ID,
		Status:      pgtype.Text{String: "cancelled", Valid: true},
		Phase:       pgtype.Text{String: "cancelled", Valid: true},
		CancelledAt: nowUTC(),
	})
	if err != nil {
		return err
	}
	steps, err := s.Queries.ListWorkflowStepsByRun(ctx, run.ID)
	if err == nil {
		for _, step := range steps {
			if step.Status == "completed" || step.Status == "cancelled" {
				continue
			}
			_, _ = s.Queries.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:     step.ID,
				Status: pgtype.Text{String: "cancelled", Valid: true},
			})
		}
	}
	s.publishWorkflowRun(updated, "cancelled")
	return nil
}

// TransitionStage moves a workflow run to a new SDD stage, updating status and
// emitting an audit event. The transition must be in allowedTransitions.
func (s *WorkflowService) TransitionStage(ctx context.Context, runID pgtype.UUID, toStage string, reason string) (db.WorkflowRun, error) {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("get workflow run: %w", err)
	}
	fromStage := run.CurrentStage
	allowed, ok := allowedTransitions[fromStage]
	if !ok {
		return db.WorkflowRun{}, fmt.Errorf("unknown stage %q", fromStage)
	}
	if !slices.Contains(allowed, toStage) {
		return db.WorkflowRun{}, fmt.Errorf("transition %q → %q is not allowed", fromStage, toStage)
	}
	newStatus, ok := stageToStatus[toStage]
	if !ok {
		return db.WorkflowRun{}, fmt.Errorf("no status mapping for stage %q", toStage)
	}
	updated, err := s.Queries.UpdateWorkflowRunStage(ctx, db.UpdateWorkflowRunStageParams{
		ID:           run.ID,
		CurrentStage: toStage,
		Status:       newStatus,
	})
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("update workflow run stage: %w", err)
	}
	_, _ = s.Queries.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID: run.ID,
		EventType:     "stage.transition",
		Payload: mustJSON(map[string]any{
			"from":   fromStage,
			"to":     toStage,
			"reason": reason,
		}),
	})
	s.publishWorkflowRun(updated, "stage_transition")
	return updated, nil
}

// enforceRunMode checks that a step does not violate the planning_only constraint.
// If the run is in planning_only mode and the step requests write access, a
// planner_violation event is recorded and an error is returned.
func (s *WorkflowService) enforceRunMode(ctx context.Context, run db.WorkflowRun, step db.WorkflowStep) error {
	if run.RunMode != "planning_only" {
		return nil
	}
	if step.WriteScope == "read_only" || step.WriteScope == "none" {
		return nil
	}
	detail := fmt.Sprintf("step %q (role=%s) requested write_scope=%q in planning_only mode", step.Title, step.Role, step.WriteScope)
	_, _ = s.Queries.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: step.ID,
		EventType:      "planner_violation",
		Payload:        mustJSON(map[string]any{"detail": detail, "step_id": util.UUIDToString(step.ID)}),
	})
	s.publishWorkflowRunByID(ctx, run.ID)
	return fmt.Errorf("planner violation: %s", detail)
}

// RejectAndRollback rejects an approval and rolls the workflow back to targetStage,
// cancelling any in-flight steps and recording a rejection event.
func (s *WorkflowService) RejectAndRollback(ctx context.Context, run db.WorkflowRun, approval db.WorkflowApproval, reviewer pgtype.UUID, comment string, targetStage string) error {
	// Validate the rollback target is a legal transition from the current stage.
	allowed, ok := allowedTransitions[run.CurrentStage]
	if !ok || !slices.Contains(allowed, targetStage) {
		// Fall back to the default rejection target if the caller didn't provide a valid one.
		if def, hasDef := stageRejectionTarget[run.CurrentStage]; hasDef {
			targetStage = def
		} else {
			return fmt.Errorf("no valid rejection target from stage %q", run.CurrentStage)
		}
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reject+rollback tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	if _, err := qtx.ResolveWorkflowApproval(ctx, db.ResolveWorkflowApprovalParams{
		ID:         approval.ID,
		Status:     "rejected",
		ReviewerID: reviewer,
		Comment:    pgtype.Text{String: comment, Valid: comment != ""},
	}); err != nil {
		return fmt.Errorf("resolve approval: %w", err)
	}

	// Cancel all in-flight steps.
	steps, err := qtx.ListWorkflowStepsByRun(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list steps: %w", err)
	}
	for _, step := range steps {
		if step.Status == "completed" || step.Status == "cancelled" || step.Status == "rejected" {
			continue
		}
		if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
			ID:     step.ID,
			Status: pgtype.Text{String: "cancelled", Valid: true},
		}); err != nil {
			return fmt.Errorf("cancel step %s: %w", util.UUIDToString(step.ID), err)
		}
	}

	newStatus, _ := stageToStatus[targetStage]
	var newReplanCount pgtype.Int4
	if strings.HasPrefix(targetStage, "planning_") {
		newReplanCount = pgtype.Int4{Int32: run.ReplanCount + 1, Valid: true}
	}
	if _, err := qtx.UpdateWorkflowRunStage(ctx, db.UpdateWorkflowRunStageParams{
		ID:           run.ID,
		CurrentStage: targetStage,
		Status:       newStatus,
		ReplanCount:  newReplanCount,
	}); err != nil {
		return fmt.Errorf("update run stage: %w", err)
	}

	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: approval.WorkflowStepID,
		EventType:      "stage.rejected",
		Payload: mustJSON(map[string]any{
			"approval_id":   util.UUIDToString(approval.ID),
			"scope":         approval.Scope,
			"from_stage":    run.CurrentStage,
			"target_stage":  targetStage,
			"comment":       comment,
			"replan_count":  run.ReplanCount + 1,
		}),
	}); err != nil {
		return fmt.Errorf("create rejection event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reject+rollback: %w", err)
	}
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowApproval(run, approval, "rejected")
	return nil
}

// WriteStateArtifact persists the current workflow state as a workflow_state
// artifact so agents and operators can inspect the full machine state.
func (s *WorkflowService) WriteStateArtifact(ctx context.Context, run db.WorkflowRun) error {
	steps, err := s.Queries.ListWorkflowStepsByRun(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list steps for state artifact: %w", err)
	}

	progress := map[string]string{}
	stageOrder := []string{
		"intake", "planning_specify", "planning_clarify", "planning_plan",
		"planning_tasks", "artifact_review", "hitl_plan_approval",
		"execution_ready", "execution", "code_review", "pr_creation", "final_hitl", "done",
	}
	for _, stage := range stageOrder {
		if stage == run.CurrentStage {
			progress[stage] = "in_progress"
		} else {
			// Determine if a stage is before or after the current one.
			stageIdx := -1
			currentIdx := -1
			for i, st := range stageOrder {
				if st == stage {
					stageIdx = i
				}
				if st == run.CurrentStage {
					currentIdx = i
				}
			}
			if stageIdx < currentIdx {
				progress[stage] = "done"
			} else {
				progress[stage] = "pending"
			}
		}
	}

	rejectionTarget := ""
	if def, ok := stageRejectionTarget[run.CurrentStage]; ok {
		rejectionTarget = def
	}

	state := map[string]any{
		"workflowId":        util.UUIDToString(run.ID),
		"issueId":           util.UUIDToString(run.IssueID),
		"currentStage":      run.CurrentStage,
		"status":            run.Status,
		"mode":              run.RunMode,
		"allowedNextStages": allowedTransitions[run.CurrentStage],
		"rejectionTarget":   rejectionTarget,
		"progress":          progress,
		"attempts": map[string]any{
			"replan":         run.ReplanCount,
			"max_replans":    run.MaxReplans,
			"max_retries":    run.MaxRetriesPerStep,
		},
		"stepCount": len(steps),
	}

	content, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	_, err = s.Queries.CreateWorkflowArtifact(ctx, db.CreateWorkflowArtifactParams{
		WorkflowRunID: run.ID,
		ArtifactType:  "workflow_state",
		Summary:       fmt.Sprintf("Stage: %s | Status: %s | Mode: %s", run.CurrentStage, run.Status, run.RunMode),
		Content:       content,
	})
	return err
}

// StartExecution transitions the workflow from hitl_plan_approval to executing,
// switching run_mode to planning_plus_execution and bootstrapping execution steps.
// It validates that a plan approval has been granted before proceeding.
func (s *WorkflowService) StartExecution(ctx context.Context, run db.WorkflowRun) (db.WorkflowRun, error) {
	if run.CurrentStage != "hitl_plan_approval" {
		return db.WorkflowRun{}, fmt.Errorf("cannot start execution from stage %q: must be in hitl_plan_approval", run.CurrentStage)
	}
	if run.RunMode != "planning_only" {
		return db.WorkflowRun{}, fmt.Errorf("workflow is already in execution mode")
	}

	// Verify a plan approval exists and is approved.
	approvals, err := s.Queries.ListWorkflowApprovalsByRun(ctx, run.ID)
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("list approvals: %w", err)
	}
	hasPlanApproval := false
	for _, a := range approvals {
		if a.Scope == "plan" && a.Status == "approved" {
			hasPlanApproval = true
			break
		}
	}
	if !hasPlanApproval {
		return db.WorkflowRun{}, fmt.Errorf("plan approval is required before starting execution")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("begin start-execution tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	// Transition run_mode and stage in one update.
	updated, err := qtx.UpdateWorkflowRunStage(ctx, db.UpdateWorkflowRunStageParams{
		ID:           run.ID,
		CurrentStage: "execution",
		Status:       "executing",
		RunMode:      pgtype.Text{String: "planning_plus_execution", Valid: true},
	})
	if err != nil {
		return db.WorkflowRun{}, fmt.Errorf("update run stage to execution: %w", err)
	}

	if err := s.bootstrapExecutionStepsTx(ctx, qtx, run.ID); err != nil {
		return db.WorkflowRun{}, fmt.Errorf("bootstrap execution steps: %w", err)
	}

	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID: run.ID,
		EventType:     "stage.transition",
		Payload: mustJSON(map[string]any{
			"from":   "hitl_plan_approval",
			"to":     "execution",
			"reason": "human approved — start execution",
		}),
	}); err != nil {
		return db.WorkflowRun{}, fmt.Errorf("create start-execution event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowRun{}, fmt.Errorf("commit start-execution: %w", err)
	}

	s.publishWorkflowRun(updated, "stage_transition")
	return updated, nil
}

func (s *WorkflowService) HandleTaskCompletion(ctx context.Context, task db.AgentTaskQueue, output string) error {
	if !task.WorkflowStepID.Valid {
		return nil
	}
	step, err := s.Queries.GetWorkflowStep(ctx, task.WorkflowStepID)
	if err != nil {
		return err
	}
	run, err := s.Queries.GetWorkflowRun(ctx, step.WorkflowRunID)
	if err != nil {
		return err
	}

	switch step.Role {
	case "planner":
		return s.materializePlannerResult(ctx, run, step, task.AgentID, output)
	default:
		return s.completeExecutionStep(ctx, run, step, task.AgentID, output)
	}
}

func (s *WorkflowService) HandleTaskFailure(ctx context.Context, task db.AgentTaskQueue, errMsg string) error {
	if !task.WorkflowStepID.Valid {
		return nil
	}
	step, err := s.Queries.GetWorkflowStep(ctx, task.WorkflowStepID)
	if err != nil {
		return err
	}
	run, err := s.Queries.GetWorkflowRun(ctx, step.WorkflowRunID)
	if err != nil {
		return err
	}
	if _, err := s.Queries.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
		ID:         step.ID,
		Status:     pgtype.Text{String: "failed", Valid: true},
		RetryCount: pgtype.Int4{Int32: step.RetryCount + 1, Valid: true},
	}); err != nil {
		return err
	}
	if _, err := s.Queries.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:     run.ID,
		Status: pgtype.Text{String: "blocked", Valid: true},
		Phase:  pgtype.Text{String: "blocked", Valid: true},
	}); err != nil {
		return err
	}
	_, _ = s.Queries.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: step.ID,
		EventType:      "step.failed",
		Payload:        mustJSON(map[string]any{"error": errMsg}),
	})
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowStep(run, step, "failed")
	return nil
}

func (s *WorkflowService) materializePlannerResult(ctx context.Context, run db.WorkflowRun, step db.WorkflowStep, agentID pgtype.UUID, output string) error {
	plan, err := parsePlannerOutput(output)
	if err != nil {
		slog.Warn("planner output parse failed", "workflow_run_id", util.UUIDToString(run.ID), "error", err)
		return s.HandleTaskFailure(ctx, db.AgentTaskQueue{WorkflowStepID: step.ID}, "planner output is not valid workflow JSON")
	}
	if len(plan.Steps) == 0 {
		return s.HandleTaskFailure(ctx, db.AgentTaskQueue{WorkflowStepID: step.ID}, "planner produced an empty workflow plan")
	}
	if len(plan.Steps) > int(run.MaxSteps) {
		return s.HandleTaskFailure(ctx, db.AgentTaskQueue{WorkflowStepID: step.ID}, "planner exceeded workflow max_steps")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	_, err = qtx.CreateWorkflowArtifact(ctx, db.CreateWorkflowArtifactParams{
		WorkflowRunID:    run.ID,
		WorkflowStepID:   step.ID,
		ArtifactType:     "plan",
		Summary:          truncate(plan.PlanSummary, 240),
		Content:          mustJSON(plan),
		CreatedByAgentID: agentID,
	})
	if err != nil {
		return err
	}

	createdSteps := make(map[string]db.WorkflowStep, len(plan.Steps))
	for i, item := range plan.Steps {
		role := normalizeRole(item.Role)
		agent, selectErr := s.selectAgentForRole(ctx, run.WorkspaceID, role, item.RequiredCapabilities, pgtype.UUID{})
		if selectErr != nil {
			return selectErr
		}
		metadata := mustJSON(map[string]any{
			"required_capabilities": item.RequiredCapabilities,
			"repo_targets":          item.RepoTargets,
			"expected_artifacts":    item.ExpectedArtifacts,
			"approval_gate":         item.ApprovalGate,
		})
		created, createErr := qtx.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
			WorkflowRunID:    run.ID,
			Role:             role,
			Title:            item.Title,
			Objective:        item.Objective,
			Status:           "draft",
			WriteScope:       normalizeWriteScope(item.WriteScope),
			RepoTarget:       firstNonEmpty(item.RepoTargets...),
			RequiresApproval: item.ApprovalGate,
			ContextVersion:   1,
			RetryCount:       0,
			AttemptCount:     0,
			SortOrder:        int32(i + 1),
			Metadata:         metadata,
			AssignedAgentID:  agent.ID,
		})
		if createErr != nil {
			return createErr
		}
		createdSteps[item.LocalID] = created
	}
	for _, dep := range plan.Deps {
		from := createdSteps[dep.From]
		to := createdSteps[dep.To]
		if !from.ID.Valid || !to.ID.Valid {
			continue
		}
		if _, err := qtx.CreateWorkflowStepEdge(ctx, db.CreateWorkflowStepEdgeParams{
			WorkflowRunID: run.ID,
			FromStepID:    from.ID,
			ToStepID:      to.ID,
		}); err != nil {
			return err
		}
	}
	if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:     run.ID,
		Status: pgtype.Text{String: "awaiting_plan_approval", Valid: true},
		Phase:  pgtype.Text{String: "plan_review", Valid: true},
	}); err != nil {
		return err
	}
	if _, err := qtx.CreateWorkflowApproval(ctx, db.CreateWorkflowApprovalParams{
		WorkflowRunID:  run.ID,
		Scope:          "plan",
		Status:         "pending",
		Comment:        "Approve the planner-generated workflow before execution begins.",
		WorkflowStepID: step.ID,
	}); err != nil {
		return err
	}
	if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
		ID:     step.ID,
		Status: pgtype.Text{String: "awaiting_approval", Valid: true},
	}); err != nil {
		return err
	}
	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: step.ID,
		EventType:      "plan.generated",
		Payload: mustJSON(map[string]any{
			"plan_summary": plan.PlanSummary,
			"step_count":   len(plan.Steps),
		}),
	}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowStep(run, step, "awaiting_approval")
	s.publishWorkflowApprovalRequest(ctx, run.ID)
	return nil
}

func (s *WorkflowService) completeExecutionStep(ctx context.Context, run db.WorkflowRun, step db.WorkflowStep, agentID pgtype.UUID, output string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	artifactType := artifactTypeForRole(step.Role)
	if _, err := qtx.CreateWorkflowArtifact(ctx, db.CreateWorkflowArtifactParams{
		WorkflowRunID:    run.ID,
		WorkflowStepID:   step.ID,
		ArtifactType:     artifactType,
		Summary:          truncate(output, 240),
		Content:          mustJSON(map[string]any{"output": output}),
		CreatedByAgentID: agentID,
	}); err != nil {
		return err
	}

	newStatus := "completed"
	runStatus := pgtype.Text{String: "executing", Valid: true}
	runPhase := pgtype.Text{String: "execution", Valid: true}
	if step.RequiresApproval {
		newStatus = "awaiting_approval"
		runStatus = pgtype.Text{String: "awaiting_handoff_approval", Valid: true}
		runPhase = pgtype.Text{String: "handoff_review", Valid: true}
		scope := "handoff"
		if step.Role == "tester" {
			scope = "finalize"
		}
		if _, err := qtx.CreateWorkflowApproval(ctx, db.CreateWorkflowApprovalParams{
			WorkflowRunID:  run.ID,
			Scope:          scope,
			Status:         "pending",
			Comment:        fmt.Sprintf("Review the %s step output before the workflow continues.", step.Role),
			WorkflowStepID: step.ID,
		}); err != nil {
			return err
		}
	}

	if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
		ID:     step.ID,
		Status: pgtype.Text{String: newStatus, Valid: true},
	}); err != nil {
		return err
	}
	if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
		ID:     run.ID,
		Status: runStatus,
		Phase:  runPhase,
	}); err != nil {
		return err
	}
	if newStatus == "completed" {
		if err := s.promoteDownstreamReadyStepsTx(ctx, qtx, run.ID, step.ID); err != nil {
			return err
		}
		if err := s.scheduleReadyStepsTx(ctx, qtx, run.ID); err != nil {
			return err
		}
		allDone, err := s.allExecutionStepsCompletedTx(ctx, qtx, run.ID)
		if err != nil {
			return err
		}
		if allDone {
			if _, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
				ID:     run.ID,
				Status: pgtype.Text{String: "completed", Valid: true},
				Phase:  pgtype.Text{String: "completed", Valid: true},
			}); err != nil {
				return err
			}
		}
	}
	if _, err := qtx.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		WorkflowRunID:  run.ID,
		WorkflowStepID: step.ID,
		EventType:      "step.completed",
		Payload: mustJSON(map[string]any{
			"role":   step.Role,
			"status": newStatus,
		}),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.publishWorkflowRunByID(ctx, run.ID)
	s.publishWorkflowStepByID(ctx, run.ID, step.ID, newStatus)
	if newStatus == "awaiting_approval" {
		s.publishWorkflowApprovalRequest(ctx, run.ID)
	}
	return nil
}

func (s *WorkflowService) promoteDownstreamReadyStepsTx(ctx context.Context, qtx *db.Queries, runID, completedStepID pgtype.UUID) error {
	edges, err := qtx.ListWorkflowStepEdgesByRun(ctx, runID)
	if err != nil {
		return err
	}
	steps, err := qtx.ListWorkflowStepsByRun(ctx, runID)
	if err != nil {
		return err
	}
	stepMap := make(map[string]db.WorkflowStep, len(steps))
	deps := map[string][]string{}
	for _, step := range steps {
		stepMap[util.UUIDToString(step.ID)] = step
	}
	for _, edge := range edges {
		fromID := util.UUIDToString(edge.FromStepID)
		toID := util.UUIDToString(edge.ToStepID)
		deps[toID] = append(deps[toID], fromID)
	}
	for _, edge := range edges {
		if edge.FromStepID != completedStepID {
			continue
		}
		targetID := util.UUIDToString(edge.ToStepID)
		target := stepMap[targetID]
		if target.Status != "draft" && target.Status != "ready" {
			continue
		}
		ready := true
		for _, depID := range deps[targetID] {
			if stepMap[depID].Status != "completed" {
				ready = false
				break
			}
		}
		if ready {
			if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
				ID:     target.ID,
				Status: pgtype.Text{String: "ready", Valid: true},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *WorkflowService) bootstrapExecutionStepsTx(ctx context.Context, qtx *db.Queries, runID pgtype.UUID) error {
	run, err := qtx.GetWorkflowRun(ctx, runID)
	if err != nil {
		return err
	}
	edges, err := qtx.ListWorkflowStepEdgesByRun(ctx, runID)
	if err != nil {
		return err
	}
	steps, err := qtx.ListWorkflowStepsByRun(ctx, runID)
	if err != nil {
		return err
	}

	inbound := map[string]int{}
	for _, edge := range edges {
		inbound[util.UUIDToString(edge.ToStepID)]++
	}

	for _, step := range steps {
		if step.Role == "planner" || step.Status != "draft" {
			continue
		}
		if inbound[util.UUIDToString(step.ID)] > 0 {
			continue
		}
		agent, err := s.Queries.GetAgent(ctx, step.AssignedAgentID)
		if err != nil {
			return err
		}
		if _, err := qtx.CreateWorkflowStepTask(ctx, db.CreateWorkflowStepTaskParams{
			AgentID:        agent.ID,
			RuntimeID:      agent.RuntimeID,
			IssueID:        run.IssueID,
			Priority:       3,
			WorkflowStepID: step.ID,
			AttemptNo:      step.AttemptCount + 1,
		}); err != nil {
			return err
		}
		if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
			ID:           step.ID,
			Status:       pgtype.Text{String: "queued", Valid: true},
			AttemptCount: pgtype.Int4{Int32: step.AttemptCount + 1, Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkflowService) scheduleReadyStepsTx(ctx context.Context, qtx *db.Queries, runID pgtype.UUID) error {
	run, err := qtx.GetWorkflowRun(ctx, runID)
	if err != nil {
		return err
	}
	steps, err := qtx.ListWorkflowStepsByRun(ctx, runID)
	if err != nil {
		return err
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].WriteScope == steps[j].WriteScope {
			return steps[i].SortOrder < steps[j].SortOrder
		}
		return steps[i].WriteScope == "repo_write"
	})

	writerLocks := map[string]bool{}
	for _, step := range steps {
		if step.Status != "ready" {
			if step.Status == "queued" || step.Status == "leased" || step.Status == "running" || step.Status == "awaiting_approval" {
				lockKey := step.RepoTarget
				if step.WriteScope == "repo_write" && lockKey != "" {
					writerLocks[lockKey] = true
				}
			}
			continue
		}
		lockKey := step.RepoTarget
		if step.WriteScope == "repo_write" && lockKey != "" && writerLocks[lockKey] {
			continue
		}
		agent, err := s.Queries.GetAgent(ctx, step.AssignedAgentID)
		if err != nil {
			return err
		}
		if _, err := qtx.CreateWorkflowStepTask(ctx, db.CreateWorkflowStepTaskParams{
			AgentID:        agent.ID,
			RuntimeID:      agent.RuntimeID,
			IssueID:        run.IssueID,
			Priority:       3,
			WorkflowStepID: step.ID,
			AttemptNo:      step.AttemptCount + 1,
		}); err != nil {
			return err
		}
		if _, err := qtx.UpdateWorkflowStepState(ctx, db.UpdateWorkflowStepStateParams{
			ID:           step.ID,
			Status:       pgtype.Text{String: "queued", Valid: true},
			AttemptCount: pgtype.Int4{Int32: step.AttemptCount + 1, Valid: true},
		}); err != nil {
			return err
		}
		if step.WriteScope == "repo_write" && lockKey != "" {
			writerLocks[lockKey] = true
		}
	}
	return nil
}

func (s *WorkflowService) allExecutionStepsCompletedTx(ctx context.Context, qtx *db.Queries, runID pgtype.UUID) (bool, error) {
	steps, err := qtx.ListWorkflowStepsByRun(ctx, runID)
	if err != nil {
		return false, err
	}
	for _, step := range steps {
		if step.Role == "planner" {
			continue
		}
		if step.Status != "completed" {
			return false, nil
		}
	}
	return true, nil
}

func (s *WorkflowService) selectAgentForRole(ctx context.Context, workspaceID pgtype.UUID, role string, requiredCaps []string, preferredAgentID pgtype.UUID) (db.Agent, error) {
	if preferredAgentID.Valid {
		agent, err := s.Queries.GetAgent(ctx, preferredAgentID)
		if err == nil && s.agentMatches(agent, role, requiredCaps) {
			return agent, nil
		}
	}
	agents, err := s.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("list agents: %w", err)
	}
	for _, agent := range agents {
		if s.agentMatches(agent, role, requiredCaps) {
			return agent, nil
		}
	}
	return db.Agent{}, fmt.Errorf("no eligible %s agent found for the workflow", role)
}

func (s *WorkflowService) agentMatches(agent db.Agent, role string, requiredCaps []string) bool {
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return false
	}
	if !slices.Contains(agent.WorkflowRoles, role) {
		return false
	}
	if len(requiredCaps) == 0 {
		return true
	}
	var caps []string
	_ = json.Unmarshal(agent.Capabilities, &caps)
	for _, cap := range requiredCaps {
		if !slices.Contains(caps, cap) {
			return false
		}
	}
	return true
}

func (s *WorkflowService) publishWorkflowRun(run db.WorkflowRun, action string) {
	payload := map[string]any{
		"run_id":       util.UUIDToString(run.ID),
		"issue_id":     util.UUIDToString(run.IssueID),
		"status":       run.Status,
		"phase":        run.Phase,
		"plan_version": run.PlanVersion,
		"action":       action,
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowRunUpdated,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload:     payload,
	})
}

func (s *WorkflowService) publishWorkflowRunByID(ctx context.Context, runID pgtype.UUID) {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err == nil {
		s.publishWorkflowRun(run, "updated")
	}
}

func (s *WorkflowService) publishWorkflowStep(run db.WorkflowRun, step db.WorkflowStep, action string) {
	payload := map[string]any{
		"run_id":            util.UUIDToString(run.ID),
		"issue_id":          util.UUIDToString(run.IssueID),
		"workflow_step_id":  util.UUIDToString(step.ID),
		"status":            step.Status,
		"role":              step.Role,
		"title":             step.Title,
		"assigned_agent_id": util.UUIDToPtr(step.AssignedAgentID),
		"action":            action,
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowStepUpdated,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload:     payload,
	})
}

func (s *WorkflowService) publishWorkflowStepByID(ctx context.Context, runID, stepID pgtype.UUID, action string) {
	run, runErr := s.Queries.GetWorkflowRun(ctx, runID)
	step, stepErr := s.Queries.GetWorkflowStep(ctx, stepID)
	if runErr == nil && stepErr == nil {
		s.publishWorkflowStep(run, step, action)
	}
}

func (s *WorkflowService) publishWorkflowApproval(run db.WorkflowRun, approval db.WorkflowApproval, action string) {
	payload := map[string]any{
		"run_id":           util.UUIDToString(run.ID),
		"issue_id":         util.UUIDToString(run.IssueID),
		"approval_id":      util.UUIDToString(approval.ID),
		"workflow_step_id": util.UUIDToPtr(approval.WorkflowStepID),
		"scope":            approval.Scope,
		"status":           approval.Status,
		"action":           action,
	}
	eventType := protocol.EventWorkflowRunUpdated
	if approval.Status == "pending" {
		eventType = protocol.EventWorkflowApprovalRequested
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload:     payload,
	})
}

func (s *WorkflowService) publishWorkflowApprovalRequest(ctx context.Context, runID pgtype.UUID) {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return
	}
	approvals, err := s.Queries.ListWorkflowApprovalsByRun(ctx, runID)
	if err != nil {
		return
	}
	for i := len(approvals) - 1; i >= 0; i-- {
		if approvals[i].Status == "pending" {
			s.publishWorkflowApproval(run, approvals[i], "pending")
			return
		}
	}
}

func (s *WorkflowService) publishWorkflowEvent(run db.WorkflowRun, stepID pgtype.UUID, eventType string, payload map[string]any) {
	payload["run_id"] = util.UUIDToString(run.ID)
	payload["issue_id"] = util.UUIDToString(run.IssueID)
	if stepID.Valid {
		payload["workflow_step_id"] = util.UUIDToString(stepID)
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload:     payload,
	})
}

func artifactTypeForRole(role string) string {
	switch role {
	case "reviewer":
		return "review_report"
	case "tester":
		return "test_report"
	case "planner":
		return "plan"
	default:
		return "diff_summary"
	}
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "planner", "coder", "reviewer", "tester":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "coder"
	}
}

func normalizeWriteScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "none", "read_only", "repo_write":
		return strings.ToLower(strings.TrimSpace(scope))
	case "write":
		return "repo_write"
	default:
		return "read_only"
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

func nowUTC() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}

var fencedJSONPattern = regexp.MustCompile("(?s)```json\\s*(\\{.*\\})\\s*```")

func parsePlannerOutput(output string) (WorkflowPlan, error) {
	text := strings.TrimSpace(output)
	if matches := fencedJSONPattern.FindStringSubmatch(text); len(matches) == 2 {
		text = matches[1]
	}
	if candidate, ok := extractJSONObject(text); ok {
		text = candidate
	}
	var plan WorkflowPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		return WorkflowPlan{}, err
	}
	if strings.TrimSpace(plan.PlanSummary) == "" {
		plan.PlanSummary = "Planner generated a workflow plan."
	}
	return plan, nil
}

func extractJSONObject(text string) (string, bool) {
	start := strings.IndexByte(text, '{')
	if start == -1 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], true
			}
		}
	}

	return "", false
}
