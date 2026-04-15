package daemon

import (
	"fmt"
	"strings"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	if task.WorkflowStep != nil {
		return buildWorkflowPrompt(task)
	}
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

func buildWorkflowPrompt(task Task) string {
	var b strings.Builder
	step := task.WorkflowStep
	b.WriteString("You are running as a workflow agent inside Multica.\n\n")
	if task.IssueID != "" {
		fmt.Fprintf(&b, "Issue ID: %s\n", task.IssueID)
	}
	fmt.Fprintf(&b, "Workflow run ID: %s\n", step.WorkflowRunID)
	fmt.Fprintf(&b, "Workflow step ID: %s\n", step.ID)
	fmt.Fprintf(&b, "Role: %s\n", step.Role)
	fmt.Fprintf(&b, "Step title: %s\n\n", step.Title)
	if step.Objective != "" {
		fmt.Fprintf(&b, "Objective:\n%s\n\n", step.Objective)
	}
	fmt.Fprintf(&b, "Write scope: %s\n", step.WriteScope)
	if step.RepoTarget != "" {
		fmt.Fprintf(&b, "Preferred repo target: %s\n", step.RepoTarget)
	}
	b.WriteString("\nStart by running `multica issue get ")
	b.WriteString(task.IssueID)
	b.WriteString(" --output json` to load the issue context.\n")
	b.WriteString("Then review the current workflow state and relevant artifacts via `multica issue runs <issue-id> --output json` and the provided workflow context.\n")
	if step.Role == "planner" {
		b.WriteString("\nPlanner output requirements:\n")
		b.WriteString("- Return JSON only. Do not add markdown fences, commentary, headings, or explanatory text before or after the JSON.\n")
		b.WriteString("- Do not post a comment to the issue and do not change the issue status.\n")
		b.WriteString("- The JSON must match this shape exactly:\n")
		b.WriteString("{\n")
		b.WriteString("  \"plan_summary\": \"short summary\",\n")
		b.WriteString("  \"base_branch\": \"main\",\n")
		b.WriteString("  \"steps\": [\n")
		b.WriteString("    {\n")
		b.WriteString("      \"id\": \"coder-step\",\n")
		b.WriteString("      \"role\": \"coder\",\n")
		b.WriteString("      \"title\": \"Implement the change\",\n")
		b.WriteString("      \"objective\": \"Describe the expected work for this step.\",\n")
		b.WriteString("      \"required_capabilities\": [\"coding\"],\n")
		b.WriteString("      \"repo_targets\": [\"https://github.com/Heldinhow/multica\"],\n")
		b.WriteString("      \"write_scope\": \"repo_write\",\n")
		b.WriteString("      \"expected_artifacts\": [\"diff_summary\"],\n")
		b.WriteString("      \"approval_gate\": true\n")
		b.WriteString("    }\n")
		b.WriteString("  ],\n")
		b.WriteString("  \"deps\": []\n")
		b.WriteString("}\n")
		b.WriteString("If the task is planning-only, still return the JSON contract with one or more read_only steps.\n")
		return b.String()
	}
	b.WriteString("Produce output that is appropriate for your role.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
func buildCommentPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		b.WriteString("[NEW COMMENT] A user just left a new comment that triggered this task. You MUST respond to THIS comment, not any previous ones:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	return b.String()
}
