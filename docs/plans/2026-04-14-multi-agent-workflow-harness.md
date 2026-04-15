# Multi-Agent Workflow Harness

## Summary

This document records the first implementation pass of native multi-agent workflows in Multica, plus the issues discovered during real-world testing with local runtimes.

The current state is no longer just an architectural sketch. The backend, daemon, database schema, APIs, and issue UI now support workflow runs with planner/coder/reviewer/tester steps. We have also run real smoke tests against the linked `https://github.com/Heldinhow/multica` repository using local `codex` and `opencode` runtimes.

The next goal is to turn this into a reliable workflow harness: a repeatable, observable, operator-friendly execution path that can survive daemon restarts, runtime quirks, and planner output variation.

## What Was Implemented

### Backend domain model

Added native workflow persistence:

- `workflow_run`
- `workflow_step`
- `workflow_step_edge`
- `workflow_artifact`
- `workflow_approval`
- `workflow_event`

Extended `agent_task_queue` with:

- `workflow_step_id`
- `attempt_no`

This gives the backend a dedicated orchestration model instead of overloading plain issue tasks.

### Workflow orchestration service

Added a deterministic foreman service in `server/internal/service/workflow.go`.

Implemented:

- create workflow run from an issue
- select agents by workflow role and capabilities
- planner step creation
- planner output materialization into steps and dependencies
- plan approval
- handoff/finalize approvals
- downstream step promotion
- ready-step scheduling
- basic write-lock behavior by repo target
- workflow cancellation
- workflow event publishing

### HTTP API and WebSocket support

Added endpoints for:

- `POST /api/issues/:id/workflows`
- `GET /api/issues/:id/workflows`
- `GET /api/workflows/:runId`
- `POST /api/workflows/:runId/approvals/:approvalId/approve`
- `POST /api/workflows/:runId/approvals/:approvalId/reject`
- `POST /api/workflows/:runId/cancel`

Added workflow events:

- `workflow:run_updated`
- `workflow:step_updated`
- `workflow:approval_requested`
- `workflow:event_created`

### Agent and daemon integration

Extended agents with:

- `workflow_roles`
- `capabilities`
- `tool_policy`

Extended daemon task payloads with workflow step context so local runtimes can understand:

- workflow run ID
- workflow step ID
- role
- write scope
- repo target
- metadata

Added workflow-aware prompt construction for planner and non-planner steps.

### Frontend

Added workflow types and query support in shared packages.

Added a workflow panel to `IssueDetail` that currently shows:

- workflow status
- phase
- step list
- attempts per step
- artifacts
- approvals
- cancel action

### Tests

Added and updated tests covering:

- workflow lifecycle create -> planner -> approval -> execution bootstrap
- approval transitions
- planner output parsing
- URL param test helper stability
- issue detail rendering safety
- conflict between assignment task queue and workflow planner queue

## Real-World Testing Performed

We ran the feature against a real self-hosted setup with:

- local PostgreSQL
- local Multica daemon
- linked workspace repo `https://github.com/Heldinhow/multica`
- local runtimes `codex` and `opencode`

### Real smoke tests completed

1. Linked the repository to the workspace.
2. Installed and authenticated the Multica CLI.
3. Registered local runtimes through the daemon.
4. Created workflow-specific agents.
5. Created real issues and workflow runs.
6. Observed planner task execution in daemon logs.
7. Verified planner task dispatch, repo checkout, and result reporting.

### Problems discovered in real runs

#### 1. Planner output did not match backend contract

Both `codex` and `opencode` produced natural-language plans instead of machine-consumable workflow JSON.

Effect:

- planner task completed at daemon level
- backend failed to parse planner result
- workflow moved to `blocked`

Fix implemented:

- planner prompt now explicitly requires JSON-only output
- planner parser now accepts:
  - plain JSON
  - fenced JSON
  - embedded JSON object with surrounding text

#### 2. Assignment-triggered issue tasks conflicted with workflow planner tasks

If an issue was already assigned to an agent, the legacy issue-task flow created a pending task for that same `(issue_id, agent_id)`.

Effect:

- workflow creation failed with:
  - `duplicate key value violates unique constraint "idx_one_pending_task_per_issue_agent"`

Fix implemented:

- `CreateRun` now cancels existing issue tasks before creating the planner task

#### 3. Daemon downtime leaves workflow steps stuck in `queued`

When the daemon was stopped, planner tasks remained queued indefinitely with no `dispatched_at` or `started_at`.

Effect:

- UI looked like workflow was hanging
- no task claiming occurred because all runtimes were offline

Operational conclusion:

- the workflow harness must treat runtime availability as first-class state
- queued-for-too-long steps should surface better diagnostics

#### 4. Workflow panel assumed arrays always existed

The issue page crashed when workflow response arrays such as `approvals` were absent.

Fix implemented:

- frontend now normalizes missing arrays to `[]`

## Current Known Behavior

The current implementation is usable but not yet a full harness.

What works now:

- create workflow run from issue
- dispatch planner task to local runtime
- parse planner result more robustly
- request human approval after valid plan generation
- schedule downstream steps after approval
- display workflow state in issue UI

What is still fragile:

- runtime/daemon availability is not surfaced clearly enough in the workflow UI
- planner behavior still depends on runtime compliance with prompt instructions
- the system still mixes legacy issue-task behavior with workflow behavior in adjacent UX paths

## What Still Needs To Be Done For a Real Workflow Harness

### 1. Daemon/runtimes health visibility

Need explicit workflow diagnostics when a step is stuck in `queued`.

Add:

- UI hint when assigned runtime is offline
- last daemon heartbeat and runtime heartbeat near each step
- warning banner for "no compatible runtime online"
- timeout/escalation for steps queued too long

### 2. Strong planner contract enforcement

The prompt and parser are better, but the harness should not depend only on best-effort prompting.

Still needed:

- planner-specific runtime policy that forbids comment/status side effects
- output channel separation between structured result and conversational logs
- planner JSON schema validation with better human-readable errors
- optional auto-retry with corrective feedback when planner output is invalid

### 3. Cleaner separation from legacy issue-task flow

Today workflows are explicit, but issue assignment can still create adjacent behavior that confuses operators.

Still needed:

- explicit product rule for when an issue is in workflow mode
- prevent assignment-triggered task creation for issues with active workflow runs
- possibly add `execution_mode` or similar issue-level flag

### 4. Retry and unblock strategy

Current failure behavior is basic.

Still needed:

- configurable retry strategy by step role
- replan flow after planner rejection or planner parse failure
- "retry step" and "retry workflow" actions in UI
- optional automatic requeue after transient daemon/runtime outages

### 5. Better scheduler guardrails

The scheduler has a first pass, but a harness needs clearer concurrency rules.

Still needed:

- stronger write-lock semantics per `(repo_target, base_branch)`
- multi-repo scheduling support
- protection against duplicate downstream enqueues
- richer handling for blocked dependency trees

### 6. Workflow transcript and observability

The UI needs to become an actual harness console.

Still needed:

- live transcript per step attempt
- aggregated timeline filtered by step/agent/artifact
- workflow event detail drawer
- display of planner raw output and parsed output for debugging
- distinction between queueing, claiming, starting, running, completion, and parse failure

### 7. Tool policy enforcement

`tool_policy` currently travels through the system but is not enforced strongly enough by all runtimes/providers.

Still needed:

- provider-level enforcement of read-only planner/tester policies
- deny issue status mutation and comment posting for planner role
- capability-based tool exposure per workflow step

### 8. End-to-end harness tests

Current tests cover service and handler behavior, but we still need fuller integration coverage.

Still needed:

- integration test for planner parse recovery from fenced/embedded JSON
- integration test for offline runtime -> queued -> online runtime -> running transition
- daemon integration test for workflow task claim and completion
- frontend tests for queued-with-offline-runtime messaging
- optional Playwright smoke test for issue workflow panel

## Recommended Next Work

### Short-term

1. Add runtime-offline diagnostics to the workflow panel.
2. Prevent planner steps from posting comments or changing issue status.
3. Auto-retry planner once with a corrective parse error message if output is invalid.
4. Add manual "retry planner" action in UI.

### Medium-term

1. Add workflow/issue execution mode separation.
2. Build transcript and event inspection tooling into the issue page.
3. Add queued-step timeout handling and clear operator messaging.
4. Expand scheduling and guardrails for multi-repo and writer conflicts.

### Longer-term

1. Full DAG visualization.
2. Structured runtime adapters for planner/coder/reviewer/tester behavior.
3. Rich harness UX for supervising long-running workflows.

## Files Touched In This Pass

Representative files:

- `server/internal/service/workflow.go`
- `server/internal/service/workflow_test.go`
- `server/internal/handler/workflow.go`
- `server/internal/handler/workflow_test.go`
- `server/internal/handler/daemon.go`
- `server/internal/daemon/prompt.go`
- `server/internal/daemon/types.go`
- `server/migrations/042_multi_agent_workflows.up.sql`
- `server/migrations/042_multi_agent_workflows.down.sql`
- `server/pkg/db/queries/workflow.sql`
- `server/pkg/db/generated/workflow.sql.go`
- `packages/core/types/workflow.ts`
- `packages/core/workflows/queries.ts`
- `packages/views/issues/components/workflow-panel.tsx`
- `packages/views/issues/components/issue-detail.tsx`

## Bottom Line

Multica now has a real first-cut multi-agent workflow system, not just a design.

But it is not yet a full workflow harness.

The harness threshold is reached when:

- runtime availability is explicit
- planner output is reliably structured
- workflow mode is cleanly separated from legacy issue tasks
- retries and operator controls exist
- queued/running/blocked states are easy to diagnose from the UI alone
