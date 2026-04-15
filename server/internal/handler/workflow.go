package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type WorkflowRunResponse struct {
	ID                string                     `json:"id"`
	WorkspaceID       string                     `json:"workspace_id"`
	IssueID           string                     `json:"issue_id"`
	Status            string                     `json:"status"`
	Phase             string                     `json:"phase"`
	PlanVersion       int32                      `json:"plan_version"`
	TokenBudget       int64                      `json:"token_budget"`
	BaseBranch        string                     `json:"base_branch"`
	CreatedBy         string                     `json:"created_by"`
	ApprovedPlanAt    *string                    `json:"approved_plan_at"`
	CancelledAt       *string                    `json:"cancelled_at"`
	MaxSteps          int32                      `json:"max_steps"`
	MaxReplans        int32                      `json:"max_replans"`
	MaxRetriesPerStep int32                      `json:"max_retries_per_step"`
	ReplanCount       int32                      `json:"replan_count"`
	RunMode           string                     `json:"run_mode"`
	CreatedAt         string                     `json:"created_at"`
	UpdatedAt         string                     `json:"updated_at"`
	Steps             []WorkflowStepResponse     `json:"steps,omitempty"`
	Edges             []WorkflowEdgeResponse     `json:"edges,omitempty"`
	Artifacts         []WorkflowArtifactResponse `json:"artifacts,omitempty"`
	Approvals         []WorkflowApprovalResponse `json:"approvals,omitempty"`
	Events            []WorkflowEventResponse    `json:"events,omitempty"`
}

type WorkflowStepResponse struct {
	ID               string                 `json:"id"`
	WorkflowRunID    string                 `json:"workflow_run_id"`
	Role             string                 `json:"role"`
	Title            string                 `json:"title"`
	Objective        string                 `json:"objective"`
	Status           string                 `json:"status"`
	WriteScope       string                 `json:"write_scope"`
	RepoTarget       string                 `json:"repo_target"`
	AssignedAgentID  *string                `json:"assigned_agent_id"`
	RequiresApproval bool                   `json:"requires_approval"`
	ContextVersion   int32                  `json:"context_version"`
	RetryCount       int32                  `json:"retry_count"`
	AttemptCount     int32                  `json:"attempt_count"`
	SortOrder        int32                  `json:"sort_order"`
	Metadata         map[string]any         `json:"metadata"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
	Attempts         []WorkflowTaskResponse `json:"attempts,omitempty"`
}

type WorkflowEdgeResponse struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflow_run_id"`
	FromStepID    string `json:"from_step_id"`
	ToStepID      string `json:"to_step_id"`
	CreatedAt     string `json:"created_at"`
}

type WorkflowArtifactResponse struct {
	ID               string         `json:"id"`
	WorkflowRunID    string         `json:"workflow_run_id"`
	WorkflowStepID   *string        `json:"workflow_step_id"`
	ArtifactType     string         `json:"artifact_type"`
	Summary          string         `json:"summary"`
	Content          map[string]any `json:"content"`
	CreatedByAgentID *string        `json:"created_by_agent_id"`
	CreatedAt        string         `json:"created_at"`
}

type WorkflowApprovalResponse struct {
	ID             string  `json:"id"`
	WorkflowRunID  string  `json:"workflow_run_id"`
	WorkflowStepID *string `json:"workflow_step_id"`
	FromStepID     *string `json:"from_step_id"`
	ToStepID       *string `json:"to_step_id"`
	Scope          string  `json:"scope"`
	Status         string  `json:"status"`
	ReviewerID     *string `json:"reviewer_id"`
	Comment        string  `json:"comment"`
	CreatedAt      string  `json:"created_at"`
	ResolvedAt     *string `json:"resolved_at"`
}

type WorkflowEventResponse struct {
	ID             string         `json:"id"`
	WorkflowRunID  string         `json:"workflow_run_id"`
	WorkflowStepID *string        `json:"workflow_step_id"`
	EventType      string         `json:"event_type"`
	Payload        map[string]any `json:"payload"`
	CreatedAt      string         `json:"created_at"`
}

type WorkflowTaskResponse struct {
	ID             string  `json:"id"`
	WorkflowStepID *string `json:"workflow_step_id"`
	Status         string  `json:"status"`
	AttemptNo      int32   `json:"attempt_no"`
	AgentID        string  `json:"agent_id"`
	RuntimeID      string  `json:"runtime_id"`
	DispatchedAt   *string `json:"dispatched_at"`
	StartedAt      *string `json:"started_at"`
	CompletedAt    *string `json:"completed_at"`
	Error          *string `json:"error"`
}

func workflowRunToResponse(run db.WorkflowRun) WorkflowRunResponse {
	return WorkflowRunResponse{
		ID:                uuidToString(run.ID),
		WorkspaceID:       uuidToString(run.WorkspaceID),
		IssueID:           uuidToString(run.IssueID),
		Status:            run.Status,
		Phase:             run.Phase,
		PlanVersion:       run.PlanVersion,
		TokenBudget:       run.TokenBudget,
		BaseBranch:        run.BaseBranch,
		CreatedBy:         uuidToString(run.CreatedBy),
		ApprovedPlanAt:    timestampToPtr(run.ApprovedPlanAt),
		CancelledAt:       timestampToPtr(run.CancelledAt),
		MaxSteps:          run.MaxSteps,
		MaxReplans:        run.MaxReplans,
		MaxRetriesPerStep: run.MaxRetriesPerStep,
		ReplanCount:       run.ReplanCount,
		RunMode:           run.RunMode,
		CreatedAt:         timestampToString(run.CreatedAt),
		UpdatedAt:         timestampToString(run.UpdatedAt),
	}
}

func workflowStepToResponse(step db.WorkflowStep) WorkflowStepResponse {
	var metadata map[string]any
	if step.Metadata != nil {
		_ = json.Unmarshal(step.Metadata, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return WorkflowStepResponse{
		ID:               uuidToString(step.ID),
		WorkflowRunID:    uuidToString(step.WorkflowRunID),
		Role:             step.Role,
		Title:            step.Title,
		Objective:        step.Objective,
		Status:           step.Status,
		WriteScope:       step.WriteScope,
		RepoTarget:       step.RepoTarget,
		AssignedAgentID:  uuidToPtr(step.AssignedAgentID),
		RequiresApproval: step.RequiresApproval,
		ContextVersion:   step.ContextVersion,
		RetryCount:       step.RetryCount,
		AttemptCount:     step.AttemptCount,
		SortOrder:        step.SortOrder,
		Metadata:         metadata,
		CreatedAt:        timestampToString(step.CreatedAt),
		UpdatedAt:        timestampToString(step.UpdatedAt),
		Attempts:         []WorkflowTaskResponse{},
	}
}

func workflowArtifactToResponse(item db.WorkflowArtifact) WorkflowArtifactResponse {
	var content map[string]any
	if item.Content != nil {
		_ = json.Unmarshal(item.Content, &content)
	}
	if content == nil {
		content = map[string]any{}
	}
	return WorkflowArtifactResponse{
		ID:               uuidToString(item.ID),
		WorkflowRunID:    uuidToString(item.WorkflowRunID),
		WorkflowStepID:   uuidToPtr(item.WorkflowStepID),
		ArtifactType:     item.ArtifactType,
		Summary:          item.Summary,
		Content:          content,
		CreatedByAgentID: uuidToPtr(item.CreatedByAgentID),
		CreatedAt:        timestampToString(item.CreatedAt),
	}
}

func workflowApprovalToResponse(item db.WorkflowApproval) WorkflowApprovalResponse {
	return WorkflowApprovalResponse{
		ID:             uuidToString(item.ID),
		WorkflowRunID:  uuidToString(item.WorkflowRunID),
		WorkflowStepID: uuidToPtr(item.WorkflowStepID),
		FromStepID:     uuidToPtr(item.FromStepID),
		ToStepID:       uuidToPtr(item.ToStepID),
		Scope:          item.Scope,
		Status:         item.Status,
		ReviewerID:     uuidToPtr(item.ReviewerID),
		Comment:        item.Comment,
		CreatedAt:      timestampToString(item.CreatedAt),
		ResolvedAt:     timestampToPtr(item.ResolvedAt),
	}
}

func workflowEventToResponse(item db.WorkflowEvent) WorkflowEventResponse {
	var payload map[string]any
	if item.Payload != nil {
		_ = json.Unmarshal(item.Payload, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return WorkflowEventResponse{
		ID:             uuidToString(item.ID),
		WorkflowRunID:  uuidToString(item.WorkflowRunID),
		WorkflowStepID: uuidToPtr(item.WorkflowStepID),
		EventType:      item.EventType,
		Payload:        payload,
		CreatedAt:      timestampToString(item.CreatedAt),
	}
}

func workflowTaskToResponse(task db.AgentTaskQueue) WorkflowTaskResponse {
	return WorkflowTaskResponse{
		ID:             uuidToString(task.ID),
		WorkflowStepID: uuidToPtr(task.WorkflowStepID),
		Status:         task.Status,
		AttemptNo:      task.AttemptNo,
		AgentID:        uuidToString(task.AgentID),
		RuntimeID:      uuidToString(task.RuntimeID),
		DispatchedAt:   timestampToPtr(task.DispatchedAt),
		StartedAt:      timestampToPtr(task.StartedAt),
		CompletedAt:    timestampToPtr(task.CompletedAt),
		Error:          textToPtr(task.Error),
	}
}

func (h *Handler) buildWorkflowRunResponse(r *http.Request, run db.WorkflowRun) (WorkflowRunResponse, error) {
	resp := workflowRunToResponse(run)
	steps, err := h.Queries.ListWorkflowStepsByRun(r.Context(), run.ID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	tasks, err := h.Queries.ListTasksByIssue(r.Context(), run.IssueID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	attemptsByStep := map[string][]WorkflowTaskResponse{}
	for _, task := range tasks {
		if !task.WorkflowStepID.Valid {
			continue
		}
		key := uuidToString(task.WorkflowStepID)
		attemptsByStep[key] = append(attemptsByStep[key], workflowTaskToResponse(task))
	}
	resp.Steps = make([]WorkflowStepResponse, len(steps))
	for i, step := range steps {
		item := workflowStepToResponse(step)
		item.Attempts = attemptsByStep[item.ID]
		resp.Steps[i] = item
	}

	edges, err := h.Queries.ListWorkflowStepEdgesByRun(r.Context(), run.ID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	resp.Edges = make([]WorkflowEdgeResponse, len(edges))
	for i, edge := range edges {
		resp.Edges[i] = WorkflowEdgeResponse{
			ID:            uuidToString(edge.ID),
			WorkflowRunID: uuidToString(edge.WorkflowRunID),
			FromStepID:    uuidToString(edge.FromStepID),
			ToStepID:      uuidToString(edge.ToStepID),
			CreatedAt:     timestampToString(edge.CreatedAt),
		}
	}

	artifacts, err := h.Queries.ListWorkflowArtifactsByRun(r.Context(), run.ID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	resp.Artifacts = make([]WorkflowArtifactResponse, len(artifacts))
	for i, item := range artifacts {
		resp.Artifacts[i] = workflowArtifactToResponse(item)
	}

	approvals, err := h.Queries.ListWorkflowApprovalsByRun(r.Context(), run.ID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	resp.Approvals = make([]WorkflowApprovalResponse, len(approvals))
	for i, item := range approvals {
		resp.Approvals[i] = workflowApprovalToResponse(item)
	}

	events, err := h.Queries.ListWorkflowEventsByRun(r.Context(), run.ID)
	if err != nil {
		return WorkflowRunResponse{}, err
	}
	resp.Events = make([]WorkflowEventResponse, len(events))
	for i, item := range events {
		resp.Events[i] = workflowEventToResponse(item)
	}

	return resp, nil
}

func (h *Handler) loadWorkflowRunForUser(w http.ResponseWriter, r *http.Request, runID string) (db.WorkflowRun, bool) {
	run, err := h.Queries.GetWorkflowRun(r.Context(), parseUUID(runID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return db.WorkflowRun{}, false
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(run.WorkspaceID), "workflow not found"); !ok {
		return db.WorkflowRun{}, false
	}
	return run, true
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		RunMode string `json:"run_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runMode := req.RunMode
	if runMode == "" {
		runMode = "planning_plus_execution"
	}

	run, _, err := h.WorkflowService.CreateRun(r.Context(), issue, parseUUID(userID), runMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.buildWorkflowRunResponse(r, run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build workflow response")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListIssueWorkflows(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	runs, err := h.Queries.ListWorkflowRunsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	resp := make([]WorkflowRunResponse, 0, len(runs))
	for _, run := range runs {
		item, buildErr := h.buildWorkflowRunResponse(r, run)
		if buildErr == nil {
			resp = append(resp, item)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	run, ok := h.loadWorkflowRunForUser(w, r, runID)
	if !ok {
		return
	}
	resp, err := h.buildWorkflowRunResponse(r, run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type WorkflowApprovalRequest struct {
	Comment string `json:"comment"`
}

func (h *Handler) ApproveWorkflowApproval(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	approvalID := chi.URLParam(r, "approvalId")
	run, ok := h.loadWorkflowRunForUser(w, r, runID)
	if !ok {
		return
	}
	approval, err := h.Queries.GetWorkflowApproval(r.Context(), parseUUID(approvalID))
	if err != nil || approval.WorkflowRunID != run.ID {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if approval.Status != "pending" {
		writeError(w, http.StatusBadRequest, "approval is already resolved")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req WorkflowApprovalRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.WorkflowService.Approve(r.Context(), run, approval, parseUUID(userID), req.Comment); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve workflow")
		return
	}
	updated, _ := h.Queries.GetWorkflowRun(r.Context(), run.ID)
	resp, err := h.buildWorkflowRunResponse(r, updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RejectWorkflowApproval(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	approvalID := chi.URLParam(r, "approvalId")
	run, ok := h.loadWorkflowRunForUser(w, r, runID)
	if !ok {
		return
	}
	approval, err := h.Queries.GetWorkflowApproval(r.Context(), parseUUID(approvalID))
	if err != nil || approval.WorkflowRunID != run.ID {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if approval.Status != "pending" {
		writeError(w, http.StatusBadRequest, "approval is already resolved")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req WorkflowApprovalRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.WorkflowService.Reject(r.Context(), run, approval, parseUUID(userID), req.Comment); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject workflow")
		return
	}
	updated, _ := h.Queries.GetWorkflowRun(r.Context(), run.ID)
	resp, err := h.buildWorkflowRunResponse(r, updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	run, ok := h.loadWorkflowRunForUser(w, r, runID)
	if !ok {
		return
	}
	if run.Status == "completed" || run.Status == "cancelled" {
		writeError(w, http.StatusBadRequest, "workflow is already finished")
		return
	}
	if err := h.WorkflowService.Cancel(r.Context(), run); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel workflow")
		return
	}
	updated, err := h.Queries.GetWorkflowRun(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow")
		return
	}
	resp, err := h.buildWorkflowRunResponse(r, updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build workflow response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
