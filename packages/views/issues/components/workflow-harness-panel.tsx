"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  Clock3,
  GitPullRequestDraft,
  Loader2,
  Play,
  RotateCcw,
  ShieldCheck,
  ShieldX,
  TimerReset,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Separator } from "@multica/ui/components/ui/separator";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { api } from "@multica/core/api";
import { issueWorkflowOptions, issueKeys } from "@multica/core/issues/queries";
import type { WorkflowReview, WorkflowRun, WorkflowSnapshot, WorkflowStage, WorkflowStatus } from "@multica/core/types/workflow";

// ─── Constants ───────────────────────────────────────────────────────────────

const orderedStages: WorkflowStage[] = [
  "intake",
  "planning_specify",
  "planning_clarify",
  "planning_plan",
  "planning_tasks",
  "artifact_review",
  "hitl_plan_approval",
  "execution",
  "code_review",
  "pr_creation",
  "final_hitl",
  "done",
];

const STAGE_LABELS: Record<WorkflowStage, string> = {
  intake: "Intake",
  planning_specify: "Specify",
  planning_clarify: "Clarify",
  planning_plan: "Draft Plan",
  planning_tasks: "Tasks",
  artifact_review: "Artifact Review",
  hitl_plan_approval: "Plan Approval",
  execution: "Implement",
  code_review: "Code Review",
  pr_creation: "PR",
  final_hitl: "Final Approval",
  done: "Done",
};

const phases = [
  { id: "planning", label: "Planning", stages: ["intake", "planning_specify", "planning_clarify", "planning_plan", "planning_tasks", "artifact_review", "hitl_plan_approval"] as WorkflowStage[] },
  { id: "execution", label: "Execution", stages: ["execution", "code_review"] as WorkflowStage[] },
  { id: "delivery", label: "Delivery", stages: ["pr_creation", "final_hitl", "done"] as WorkflowStage[] },
];

const PARTICIPANTS = [
  { role: "Orchestrator", stages: ["intake"], artifact: ".workflow/state.json", note: "Coordinates transitions" },
  { role: "Planner", stages: ["planning_specify", "planning_clarify", "planning_plan", "planning_tasks"], artifact: "spec · plan · tasks", note: "No implementation" },
  { role: "Artifact Critic", stages: ["artifact_review"], artifact: "reviews/artifact-review.md", note: "Reviews planning artifacts" },
  { role: "HITL Gate", stages: ["hitl_plan_approval", "final_hitl"], artifact: ".workflow/handoff.md", note: "Human approval gate" },
  { role: "Executor", stages: ["execution"], artifact: "progress.md · evidence/", note: "Implements approved tasks" },
  { role: "Code Reviewer", stages: ["code_review"], artifact: "reviews/code-review.md", note: "Validates against spec" },
  { role: "PR Creator", stages: ["pr_creation"], artifact: "draft PR · stage-result.json", note: "Draft PR link" },
  { role: "Progress Tracker", stages: orderedStages as WorkflowStage[], artifact: "state · handoff · progress", note: "Persists checkpoints" },
];

// ─── Helpers ─────────────────────────────────────────────────────────────────

function labelize(value?: string | null) {
  if (!value) return "—";
  return value.replaceAll("_", " ");
}

function formatRelativeTime(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function stageStatus(run: WorkflowRun, stage: WorkflowStage): "done" | "current" | "pending" | "waiting" | "blocked" | "error" {
  const done = run.progress[stage] === "done";
  if (done) return "done";
  const isCurrent = stage === run.currentStage;
  if (isCurrent) return run.status === "blocked" ? "blocked" : "current";
  const isWaiting = run.progress[stage] === "waiting_approval" || run.progress[stage] === "waiting_review";
  if (isWaiting) return "waiting";
  return "pending";
}

// ─── Status token system ─────────────────────────────────────────────────────

function StatusDot({ status }: { status: WorkflowStatus }) {
  switch (status) {
    case "done":
    case "approved":
      return <span className="relative flex h-2 w-2"><span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" /><span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" /></span>;
    case "running":
      return <span className="relative flex h-2 w-2"><span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75" /><span className="relative inline-flex h-2 w-2 rounded-full bg-blue-500" /></span>;
    case "waiting_approval":
    case "waiting_review":
      return <span className="relative flex h-2 w-2"><span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75" /><span className="relative inline-flex h-2 w-2 rounded-full bg-amber-500" /></span>;
    case "rejected":
    case "blocked":
      return <span className="relative flex h-2 w-2"><span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-400 opacity-75" /><span className="relative inline-flex h-2 w-2 rounded-full bg-red-500" /></span>;
  }
}

function statusBadgeClass(status: WorkflowStatus) {
  switch (status) {
    case "done":
    case "approved":
      return "bg-emerald-500/10 text-emerald-500 border-emerald-500/20";
    case "running":
      return "bg-blue-500/10 text-blue-500 border-blue-500/20";
    case "waiting_approval":
    case "waiting_review":
      return "bg-amber-500/10 text-amber-500 border-amber-500/20";
    case "rejected":
    case "blocked":
      return "bg-red-500/10 text-red-500 border-red-500/20";
  }
}

// ─── Sub-components ───────────────────────────────────────────────────────────

/** Step dot inside a phase row */
function StageStep({ stage, run }: { stage: WorkflowStage; run: WorkflowRun }) {
  const status = stageStatus(run, stage);
  const label = STAGE_LABELS[stage] ?? labelize(stage);
  const isCurrent = stage === run.currentStage;
  const isError = run.currentError != null && isCurrent;

  return (
    <div className={cn(
      "flex items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors",
      status === "done" && "bg-emerald-500/5",
      status === "current" && "bg-blue-500/10",
      status === "waiting" && "bg-amber-500/5",
      status === "blocked" && "bg-red-500/5",
      status === "error" && "bg-red-500/5",
      status === "pending" && "",
    )}>
      {/* Icon */}
      <span className={cn(
        "flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px]",
        status === "done" && "bg-emerald-500/20 text-emerald-500",
        status === "current" && "bg-blue-500/20 text-blue-500",
        status === "waiting" && "bg-amber-500/20 text-amber-500",
        status === "blocked" && "bg-red-500/20 text-red-500",
        status === "error" && "bg-red-500/20 text-red-500",
        status === "pending" && "bg-muted text-muted-foreground",
      )}>
        {status === "done" && <Check className="h-2.5 w-2.5" />}
        {status === "current" && <Loader2 className="h-2.5 w-2.5 animate-spin" />}
        {status === "waiting" && <Clock3 className="h-2.5 w-2.5" />}
        {status === "blocked" && <XCircle className="h-2.5 w-2.5" />}
        {status === "error" && <AlertCircle className="h-2.5 w-2.5" />}
        {status === "pending" && <Circle className="h-2.5 w-2.5" />}
      </span>
      {/* Label */}
      <span className={cn(
        "text-xs font-medium",
        status === "done" && "text-emerald-600 dark:text-emerald-400",
        status === "current" && "text-blue-600 dark:text-blue-400",
        status === "waiting" && "text-amber-600 dark:text-amber-400",
        status === "blocked" && "text-red-600 dark:text-red-400",
        status === "error" && "text-red-600 dark:text-red-400",
        status === "pending" && "text-muted-foreground",
      )}>
        {label}
      </span>
      {/* Error message on current error stage */}
      {isError && run.currentError && (
        <span className="ml-1 truncate text-[10px] text-red-500">{run.currentError}</span>
      )}
    </div>
  );
}

/** Collapsible phase row */
function PhaseRow({ phase, run }: { phase: (typeof phases)[0]; run: WorkflowRun }) {
  const [open, setOpen] = useState(phases.findIndex(p => p.stages.includes(run.currentStage)) === phases.findIndex(p => p.id === phase.id));
  const doneCount = phase.stages.filter(s => run.progress[s] === "done").length;
  const allDone = doneCount === phase.stages.length;
  const hasCurrent = phase.stages.includes(run.currentStage);
  const lastStage = phase.stages[phase.stages.length - 1];
  const currentProgress = lastStage && run.progress[lastStage] === "done" ? 100
    : phase.stages.indexOf(run.currentStage) >= 0
      ? Math.round((phase.stages.findIndex(s => s === run.currentStage) / phase.stages.length) * 100)
      : 0;

  return (
    <div className={cn(
      "rounded-lg border transition-colors",
      allDone && "border-emerald-500/10 bg-emerald-500/5",
      hasCurrent && !allDone && "border-blue-500/20 bg-blue-500/5",
      !hasCurrent && !allDone && "border-border bg-card",
    )}>
      {/* Phase header */}
      <button
        onClick={() => setOpen(o => !o)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left"
      >
        {/* Phase icon badge */}
        <div className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-xs font-semibold",
          allDone && "bg-emerald-500/15 text-emerald-500",
          hasCurrent && !allDone && "bg-blue-500/15 text-blue-500",
          !hasCurrent && !allDone && "bg-muted text-muted-foreground",
        )}>
          {allDone ? <CheckCircle2 className="h-4 w-4" /> : hasCurrent ? <Loader2 className="h-4 w-4 animate-spin" /> : <Circle className="h-3.5 w-3.5" />}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className={cn(
              "text-sm font-semibold",
              allDone && "text-emerald-600 dark:text-emerald-400",
              hasCurrent && !allDone && "text-foreground",
              !hasCurrent && !allDone && "text-muted-foreground",
            )}>
              {phase.label}
            </span>
          </div>
          <div className="mt-1 flex items-center gap-2">
            {/* Mini progress bar */}
            <div className="h-1 w-16 rounded-full bg-muted">
              <div className={cn(
                "h-1 rounded-full transition-all",
                allDone ? "bg-emerald-500 w-full" : hasCurrent ? "bg-blue-500" : "bg-muted-foreground/30",
              )} style={{ width: `${allDone ? 100 : currentProgress}%` }} />
            </div>
            <span className="text-[10px] text-muted-foreground">
              {doneCount}/{phase.stages.length}
            </span>
          </div>
        </div>

        <ChevronDown className={cn(
          "h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200",
          open && "rotate-180",
        )} />
      </button>

      {/* Stage steps — only when open */}
      {open && (
        <div className="border-t border-border px-4 pb-3 pt-2">
          <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3 md:grid-cols-4">
            {phase.stages.map(stage => (
              <StageStep key={stage} stage={stage} run={run} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

/** Workflow participants grid */
function ParticipantsPanel({ run }: { run: WorkflowRun }) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="h-4 w-4 rounded-sm bg-primary/20" />
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Participants</span>
      </div>
      <div className="grid grid-cols-2 gap-2">
        {PARTICIPANTS.map(p => {
          const isActive = p.stages.includes(run.currentStage);
          const doneStages = p.stages.filter(s => run.progress[s] === "done").length;
          const allDone = doneStages === p.stages.length;

          return (
            <div key={p.role} className={cn(
              "rounded-lg border p-3 transition-colors",
              isActive && !allDone && "border-blue-500/30 bg-blue-500/5",
              allDone && "border-emerald-500/20 bg-emerald-500/5",
              !isActive && !allDone && "border-border bg-card",
            )}>
              {/* Role */}
              <div className="flex items-center justify-between gap-2">
                <span className={cn(
                  "text-xs font-semibold",
                  isActive && !allDone && "text-blue-600 dark:text-blue-400",
                  allDone && "text-emerald-600 dark:text-emerald-400",
                  !isActive && !allDone && "text-foreground",
                )}>
                  {p.role}
                </span>
                {isActive && !allDone && <span className="h-1.5 w-1.5 rounded-full bg-blue-500 animate-pulse" />}
                {allDone && <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />}
              </div>
              {/* Note */}
              <p className="mt-0.5 text-[10px] text-muted-foreground leading-relaxed">{p.note}</p>
              {/* Artifact */}
              <div className="mt-1.5 rounded border border-border/50 bg-muted/30 px-1.5 py-0.5">
                <span className="text-[10px] font-mono text-muted-foreground">{p.artifact}</span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** StatePanel — inlined in main render */

/** Activity audit trail */
function ActivityFeed({ snapshot }: { snapshot: WorkflowSnapshot }) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="h-4 w-4 rounded-sm bg-primary/20" />
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Activity</span>
      </div>
      <div className="relative">
        {/* Vertical timeline line */}
        <div className="absolute left-[11px] top-3 h-[calc(100%-12px)] w-px bg-gradient-to-b from-border to-transparent" />
        <div className="space-y-2 pl-8">
          {snapshot.transitions.slice(0, 12).map((t, i) => {
            const isLatest = i === 0;
            const isHuman = t.actorType === "human" || t.action === "approve" || t.action === "reject";
            const isError = t.action === "retry" || t.action === "blocked" || t.action === "error";
            const dotColor = isError ? "bg-red-500" : isHuman ? "bg-blue-500" : "bg-muted-foreground/40";
            const textColor = isError ? "text-red-500" : isHuman ? "text-blue-500" : "text-muted-foreground";

            return (
              <div key={t.id} className="relative">
                <span className={cn("absolute -left-[17px] top-1.5 flex h-5 w-5 items-center justify-center rounded-full border", isLatest ? `border-${textColor} bg-${textColor}/10` : "border-border bg-card")}>
                  <span className={cn("h-1.5 w-1.5 rounded-full", dotColor)} />
                </span>
                <div className={cn("flex items-start justify-between gap-2 rounded-md border border-transparent px-2 py-1.5 transition-colors hover:bg-muted/20", isLatest && "border-border/50 bg-muted/20")}>
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className={cn("text-xs font-medium", textColor)}>{labelize(t.action)}</span>
                      <span className="text-[10px] text-muted-foreground">→ {labelize(t.toStage)}</span>
                    </div>
                    <div className="flex items-center gap-1 mt-0.5">
                      {isHuman && <span className="text-[10px] text-blue-400">by {t.actorType}</span>}
                      {t.reason && <span className="text-[10px] italic text-muted-foreground truncate max-w-[160px]">"{t.reason}"</span>}
                    </div>
                  </div>
                  <span className="text-[10px] text-muted-foreground shrink-0">{formatTime(t.createdAt)}</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** HITL gate — approval/rejection panel */
function HitlGate({ run, onTransition, isPending, reason, setReason }: {
  run: WorkflowRun;
  onTransition: (action: string, target?: WorkflowStage) => void;
  isPending: boolean;
  reason: string;
  setReason: (v: string) => void;
}) {
  const waitingPlanApproval = run.currentStage === "hitl_plan_approval";
  const waitingFinalApproval = run.currentStage === "final_hitl";
  const canStartExecution = waitingPlanApproval && run.status === "approved";

  if (!waitingPlanApproval && !waitingFinalApproval && !canStartExecution) return null;

  return (
    <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-4">
      <div className="flex items-center gap-2 mb-3">
        <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-amber-500/10">
          <TimerReset className="h-4 w-4 text-amber-500" />
        </div>
        <div>
          <div className="text-sm font-semibold">Human-in-the-Loop Gate</div>
          <div className="text-xs text-muted-foreground">
            {canStartExecution ? "Plan approved — ready to execute" : "Awaiting human decision"}
          </div>
        </div>
      </div>

      <Textarea
        value={reason}
        onChange={e => setReason(e.target.value)}
        placeholder="Optional reason (visible in audit trail)"
        className="mb-3 text-xs"
        rows={2}
      />

      <div className="flex flex-wrap items-center gap-2">
        {waitingPlanApproval && run.status === "waiting_approval" && (
          <>
            <Button size="sm" onClick={() => onTransition("approve")} disabled={isPending} className="gap-1.5 bg-emerald-600 hover:bg-emerald-700">
              <ShieldCheck className="h-3.5 w-3.5" />
              Approve Plan
            </Button>
            <Button size="sm" variant="outline" onClick={() => onTransition("send_back", "planning_tasks")} disabled={isPending} className="gap-1.5">
              <RotateCcw className="h-3.5 w-3.5" />
              Send Back
            </Button>
          </>
        )}
        {canStartExecution && (
          <Button size="sm" onClick={() => onTransition("start_execution")} disabled={isPending} className="gap-1.5 bg-blue-600 hover:bg-blue-700">
            <Play className="h-3.5 w-3.5" />
            Start Execution
          </Button>
        )}
        {waitingFinalApproval && run.status === "waiting_approval" && (
          <>
            <Button size="sm" onClick={() => onTransition("approve")} disabled={isPending} className="gap-1.5 bg-emerald-600 hover:bg-emerald-700">
              <ShieldCheck className="h-3.5 w-3.5" />
              Approve
            </Button>
            <Button size="sm" variant="outline" onClick={() => onTransition("send_back", "code_review")} disabled={isPending} className="gap-1.5">
              <RotateCcw className="h-3.5 w-3.5" />
              Send Back to Review
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

/** Review findings panel */
function ReviewFindingsPanel({ review }: { review: WorkflowReview }) {
  const [expanded, setExpanded] = useState(false);
  if (!review || (review.findings?.length === 0 && review.decision !== "rejected")) return null;

  return (
    <div className={cn(
      "rounded-lg border p-4",
      review.decision === "rejected" ? "border-red-500/20 bg-red-500/5" : "border-amber-500/20 bg-amber-500/5",
    )}>
      <div className="flex items-center gap-2 mb-2">
        <div className={cn(
          "flex h-7 w-7 items-center justify-center rounded-lg",
          review.decision === "rejected" ? "bg-red-500/10" : "bg-amber-500/10",
        )}>
          {review.decision === "rejected" ? (
            <ShieldX className="h-4 w-4 text-red-500" />
          ) : (
            <AlertTriangle className="h-4 w-4 text-amber-500" />
          )}
        </div>
        <div>
          <div className="text-sm font-semibold">Review Findings</div>
          <div className={cn("text-xs", review.decision === "rejected" ? "text-red-400" : "text-amber-400")}>
            {review.decision === "rejected" ? "Rejected" : "Approved"} · {review.findings?.length ?? 0} finding{(review.findings?.length ?? 0) !== 1 ? "s" : ""}
          </div>
        </div>
      </div>
      {review.summary && (
        <p className="mb-3 text-xs text-muted-foreground">{review.summary}</p>
      )}
      {(review.findings?.length ?? 0) > 0 && (
        <div className="space-y-1.5">
          <button
            onClick={() => setExpanded(e => !e)}
            className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground"
          >
            {expanded ? <ChevronDown className="h-3 w-3 rotate-180" /> : <ChevronRight className="h-3 w-3" />}
            {expanded ? "Hide" : "Show"} {(review.findings?.length ?? 0)} finding{(review.findings?.length ?? 0) !== 1 ? "s" : ""}
          </button>
          {expanded && (review.findings ?? []).map((f, i) => (
            <div key={i} className={cn(
              "rounded border p-2",
              f.severity === "error" ? "border-red-500/20 bg-red-500/5" : "border-border bg-muted/30",
            )}>
              <div className="flex items-center gap-1.5">
                {f.severity === "error" && <XCircle className="h-3 w-3 text-red-500" />}
                <span className="text-xs font-medium">{f.title}</span>
              </div>
              <p className="mt-0.5 text-[10px] text-muted-foreground">{f.body}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Main component ───────────────────────────────────────────────────────────

export function WorkflowHarnessPanel({ issueId }: { issueId: string }) {
  const qc = useQueryClient();
  const [reason, setReason] = useState("");
  const { data: snapshot, error, isLoading } = useQuery(issueWorkflowOptions(issueId));

  const invalidate = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: issueKeys.workflow(issueId) }),
      qc.invalidateQueries({ queryKey: issueKeys.workflowHistory(issueId) }),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) }),
    ]);
  };

  const startWorkflow = useMutation({
    mutationFn: () => api.startIssueWorkflow(issueId),
    onSuccess: async (result) => {
      await invalidate();
      toast.success(result.blocked ? (result.message ?? "Workflow blocked") : "Workflow started");
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to start workflow");
    },
  });

  const transition = useMutation({
    mutationFn: (payload: { action: string; rejection_target?: WorkflowStage }) =>
      api.transitionIssueWorkflow(issueId, {
        action: payload.action,
        reason: reason.trim() || undefined,
        rejection_target: payload.rejection_target,
      }),
    onSuccess: async (_, variables) => {
      await invalidate();
      setReason("");
      toast.success(variables.action === "start_execution" ? "Execution started" : "Workflow updated");
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to update workflow");
    },
  });

  const progress = useMemo(() => {
    if (!snapshot) return { done: 0, total: orderedStages.length, percent: 0, humanDone: 0, humanTotal: 0, humanPercent: 0 };
    const HUMAN_STAGES = orderedStages.filter(s => s !== "intake");
    const humanDone = HUMAN_STAGES.filter(s => snapshot.run.progress[s] === "done").length;
    const humanPercent = Math.round((humanDone / HUMAN_STAGES.length) * 100);
    const done = Object.values(snapshot.run.progress).filter((status) => status === "done").length;
    return { done, total: orderedStages.length, percent: Math.round((done / orderedStages.length) * 100), humanDone, humanTotal: HUMAN_STAGES.length, humanPercent };
  }, [snapshot]);

  const review = snapshot?.reviews?.[0] ?? null;

  // Handler for HITL transitions
  const handleTransition = (action: string, rejection_target?: WorkflowStage) => {
    transition.mutate({ action, rejection_target });
  };

  // Empty / loading / error states
  if (isLoading || !snapshot) {
    return (
      <div className="mt-6 rounded-xl border border-border bg-card">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div className="h-5 w-40 animate-pulse rounded-md bg-muted" />
          <div className="h-5 w-24 animate-pulse rounded-md bg-muted" />
        </div>
        <div className="grid gap-4 p-5 lg:grid-cols-[1fr_340px]">
          <div className="space-y-4">
            {[1, 2, 3].map(i => <div key={i} className="h-20 animate-pulse rounded-lg bg-muted" />)}
          </div>
          <div className="space-y-3">
            {[1, 2, 3].map(i => <div key={i} className="h-16 animate-pulse rounded-lg bg-muted" />)}
          </div>
        </div>
      </div>
    );
  }

  if (error && !(error instanceof Error && error.message.toLowerCase().includes("workflow not found"))) {
    return (
      <Card className="mt-6 border-red-500/20">
        <CardHeader>
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-red-500" />
            <CardTitle className="text-base">Workflow Harness</CardTitle>
          </div>
          <CardDescription>Failed to load workflow state.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm text-muted-foreground">{error instanceof Error ? error.message : "Unexpected error"}</p>
          <Button variant="outline" size="sm" onClick={() => void invalidate()}>
            Retry
          </Button>
        </CardContent>
      </Card>
    );
  }

  if (!snapshot) {
    return (
      <Card className="mt-6 border-dashed">
        <CardHeader>
          <CardTitle className="text-base">Workflow Harness</CardTitle>
          <CardDescription>Start the harness-based SDD flow for this issue.</CardDescription>
        </CardHeader>
        <CardContent className="flex items-center justify-between gap-4">
          <p className="text-sm text-muted-foreground">
            Planning, artifact review, HITL approval, execution, code review and PR creation will be tracked here.
          </p>
          <Button onClick={() => startWorkflow.mutate()} disabled={startWorkflow.isPending}>
            <Play className="mr-2 h-4 w-4" />
            Start Workflow
          </Button>
        </CardContent>
      </Card>
    );
  }

  const run = snapshot.run;

  // ── Main render ──────────────────────────────────────────────────────────

  return (
    <div className="mt-6 space-y-4">
      {/* ── Workflow Hero ──────────────────────────────────────────────────── */}
      <div className={cn(
        "relative overflow-hidden rounded-xl border bg-card pl-4",
        run.status === "done" || run.status === "approved" ? "border-emerald-500/20" :
        run.status === "blocked" || run.status === "rejected" ? "border-red-500/20" :
        run.status === "waiting_approval" || run.status === "waiting_review" ? "border-amber-500/20" :
        "border-blue-500/20",
      )}>
        {/* Left accent bar */}
        <div className={cn(
          "absolute left-0 top-0 h-full w-1",
          (run.status === "done" || run.status === "approved") && "bg-emerald-500",
          (run.status === "blocked" || run.status === "rejected") && "bg-red-500",
          (run.status === "waiting_approval" || run.status === "waiting_review") && "bg-amber-500",
          (run.status === "running") && "bg-blue-500",
        )} />

        <div className="flex flex-wrap items-start justify-between gap-4 px-5 py-4 pl-5">
          {/* Left: title + meta */}
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="text-base font-semibold text-foreground">Workflow Harness</h2>
              <Badge className={cn("text-[10px] h-5 border px-2 font-medium", statusBadgeClass(run.status))}>
                <StatusDot status={run.status} />
                <span className="ml-1.5">{labelize(run.status)}</span>
              </Badge>
              <Badge variant="outline" className="text-[10px] h-5 px-2 font-normal">{labelize(run.mode)}</Badge>
            </div>
            <div className="mt-1.5 flex items-center gap-3 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <Clock3 className="h-3 w-3" />
                {formatRelativeTime(run.updatedAt)}
              </span>
              <Separator orientation="vertical" className="h-3" />
              <span>Stage: <span className="font-medium text-foreground">{STAGE_LABELS[run.currentStage]}</span></span>
              {run.currentError && (
                <>
                  <Separator orientation="vertical" className="h-3" />
                  <span className="flex items-center gap-1 text-red-500">
                    <AlertCircle className="h-3 w-3" />
                    {run.currentError}
                  </span>
                </>
              )}
            </div>
          </div>

          {/* Right: progress + CTA */}
          <div className="flex items-center gap-4">
            {/* Progress bar */}
            <div className="hidden sm:flex items-center gap-2">
              <div className="h-1.5 w-28 rounded-full bg-muted">
                <div
                  className={cn(
                    "h-1.5 rounded-full transition-all",
                    (run.status === "done" || run.status === "approved") ? "bg-emerald-500" :
                    (run.status === "blocked" || run.status === "rejected") ? "bg-red-500" :
                    (run.status === "waiting_approval" || run.status === "waiting_review") ? "bg-amber-500" :
                    "bg-blue-500",
                  )}
                  style={{ width: `${progress.humanPercent}%` }}
                />
              </div>
              <span className="text-xs font-semibold tabular-nums">{progress.humanPercent}%</span>
            </div>

            {/* CTA */}
            {(run.status === "blocked") && (
              <Button size="sm" variant="outline" onClick={() => transition.mutate({ action: "retry" })} disabled={transition.isPending} className="gap-1.5">
                <RotateCcw className="h-3.5 w-3.5" />
                Retry
              </Button>
            )}
            {(run.status === "running" || run.status === "done") && (
              <Button size="sm" variant="outline" onClick={() => void invalidate()} className="gap-1.5">
                <Loader2 className={cn("h-3.5 w-3.5", transition.isPending && "animate-spin")} />
                Refresh
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* ── Main layout: timeline + sidebar ────────────────────────────────── */}
      <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
        {/* Left column */}
        <div className="space-y-4">
          {/* Phase timeline */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <div className="h-4 w-4 rounded-sm bg-primary/20" />
              <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Workflow Phases</span>
            </div>
            {phases.map(phase => (
              <PhaseRow key={phase.id} phase={phase} run={run} />
            ))}
          </div>

          {/* Participants */}
          <ParticipantsPanel run={run} />

          {/* Review findings */}
          {review && <ReviewFindingsPanel review={review} />}

          {/* HITL gate */}
          <HitlGate
            run={run}
            onTransition={handleTransition}
            isPending={transition.isPending}
            reason={reason}
            setReason={setReason}
          />

          {/* Activity feed */}
          <ActivityFeed snapshot={snapshot} />
        </div>

        {/* Right column: State Panel */}
        <div>
          <div className="sticky top-4 space-y-3">
            {/* State Details header */}
            <div className="flex items-center gap-2 px-1">
              <div className="h-4 w-4 rounded-sm bg-primary/20" />
              <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">State Details</span>
            </div>

            {/* Current stage — prominent */}
            <div className={cn(
              "rounded-lg border p-4",
              run.status === "done" || run.status === "approved" ? "border-emerald-500/20 bg-emerald-500/5" :
              run.status === "blocked" || run.status === "rejected" ? "border-red-500/20 bg-red-500/5" :
              run.status === "waiting_approval" || run.status === "waiting_review" ? "border-amber-500/20 bg-amber-500/5" :
              "border-blue-500/20 bg-blue-500/5",
            )}>
              <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Current Stage</div>
              <div className="mt-1 text-base font-semibold">{STAGE_LABELS[run.currentStage] ?? labelize(run.currentStage)}</div>
              <div className="mt-1.5 flex items-center gap-1.5">
                <StatusDot status={run.status} />
                <span className="text-xs text-muted-foreground">{labelize(run.status)}</span>
                {run.mode && (
                  <>
                    <span className="text-muted-foreground/30">·</span>
                    <span className="text-xs text-muted-foreground">{labelize(run.mode)}</span>
                  </>
                )}
              </div>
              {run.rejectionTarget && (
                <div className="mt-2 flex items-center justify-between border-t border-border/50 pt-2">
                  <span className="text-[10px] text-muted-foreground">Rollback target</span>
                  <span className="text-xs font-medium text-amber-500">{labelize(run.rejectionTarget)}</span>
                </div>
              )}
              <div className="mt-2 flex items-center justify-between border-t border-border/50 pt-2">
                <span className="text-[10px] text-muted-foreground">Updated</span>
                <span className="text-xs text-muted-foreground">{formatRelativeTime(run.updatedAt)}</span>
              </div>
            </div>

            {/* Metrics grid */}
            <div className="rounded-lg border border-border bg-card p-4">
              <div className="grid grid-cols-2 gap-3">
                {[
                  { label: "Replans", value: run.attempts.replan ?? 0 },
                  { label: "Retries", value: run.attempts.executionRetry ?? 0 },
                  { label: "Progress", value: `${progress.percent}%` },
                  { label: "Human Stages", value: `${progress.humanDone}/${progress.humanTotal}` },
                ].map(({ label, value }) => (
                  <div key={label}>
                    <div className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
                    <div className="mt-0.5 text-sm font-semibold tabular-nums">{value}</div>
                  </div>
                ))}
              </div>
              {/* Progress bar */}
              <div className="mt-3 h-1 w-full rounded-full bg-muted">
                <div className={cn(
                  "h-1 rounded-full transition-all",
                  (run.status === "done" || run.status === "approved") ? "bg-emerald-500" :
                  (run.status === "blocked" || run.status === "rejected") ? "bg-red-500" :
                  (run.status === "waiting_approval" || run.status === "waiting_review") ? "bg-amber-500" :
                  "bg-blue-500",
                )} style={{ width: `${progress.humanPercent}%` }} />
              </div>
            </div>

            {/* Artifacts */}
            {(() => {
              const artifacts = [run.artifacts.spec, run.artifacts.plan, run.artifacts.tasks].filter(Boolean);
              if (artifacts.length === 0 && !run.currentPrUrl) return null;
              return (
                <div className="rounded-lg border border-border bg-card p-4">
                  <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground mb-2">Artifacts</div>
                  <div className="flex flex-wrap gap-1.5">
                    {artifacts.map(a => (
                      <span key={a} className="rounded border border-border bg-muted/40 px-2 py-0.5 font-mono text-[10px] text-muted-foreground">{a}</span>
                    ))}
                    {run.currentPrUrl && (
                      <a href={run.currentPrUrl} target="_blank" rel="noreferrer" className="flex items-center gap-1 rounded border border-blue-500/20 bg-blue-500/5 px-2 py-0.5 font-mono text-[10px] text-blue-500 hover:underline">
                        <GitPullRequestDraft className="h-3 w-3" />
                        PR Link
                      </a>
                    )}
                  </div>
                </div>
              );
            })()}

            {/* Handoff */}
            {run.handoff && (
              <HandoffSection handoff={run.handoff} />
            )}

            {/* Execution history */}
            <ExecutionHistoryPanel transitions={snapshot.transitions} />
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Sub-components (sibling definitions to avoid hoisting issues) ───────────

function HandoffSection({ handoff }: { handoff: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="rounded-lg border border-border bg-card">
      <button
        onClick={() => setOpen(o => !o)}
        className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/30 transition-colors"
      >
        <div className="flex items-center gap-2">
          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
          <span className="text-xs font-semibold">Handoff Doc</span>
        </div>
        <ChevronDown className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform duration-200", open && "rotate-180")} />
      </button>
      {open && (
        <div className="border-t border-border px-4 pb-3 pt-2">
          <pre className="whitespace-pre-wrap rounded border border-border/50 bg-muted/30 p-3 text-[10px] leading-relaxed text-muted-foreground">{handoff}</pre>
        </div>
      )}
    </div>
  );
}

function ExecutionHistoryPanel({ transitions }: { transitions: WorkflowSnapshot["transitions"] }) {
  const [open, setOpen] = useState(false);

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <button
        onClick={() => setOpen(o => !o)}
        className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/30 transition-colors"
      >
        <div className="flex items-center gap-2">
          <Clock3 className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-xs font-semibold">Execution History</span>
          <Badge variant="secondary" className="text-[10px] h-4 px-1.5">{transitions.length}</Badge>
        </div>
        <ChevronDown className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform duration-200", open && "rotate-180")} />
      </button>
      {open && (
        <div className="border-t border-border">
          <ScrollArea className="h-56">
            <div className="p-3 space-y-2">
              {transitions.length === 0 ? (
                <p className="text-xs text-muted-foreground py-2">No transitions yet.</p>
              ) : (
                [...transitions].reverse().map((t) => {
                  const isHuman = t.actorType === "human";
                  const isError = t.action === "retry" || t.action === "blocked";
                  return (
                    <div key={t.id} className="flex items-start gap-2.5 rounded-md border border-transparent px-2 py-1.5 transition-colors hover:bg-muted/20">
                      <div className={cn(
                        "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[8px]",
                        isError ? "bg-red-500/10 text-red-500" : isHuman ? "bg-blue-500/10 text-blue-500" : "bg-muted text-muted-foreground",
                      )}>
                        {isError ? <XCircle className="h-2.5 w-2.5" /> : isHuman ? <CheckCircle2 className="h-2.5 w-2.5" /> : <Circle className="h-2 w-2" />}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-1">
                          <span className="text-xs font-medium">{labelize(t.action)}</span>
                          <span className="text-[10px] text-muted-foreground">{formatTime(t.createdAt)}</span>
                        </div>
                        <div className="text-[10px] text-muted-foreground">
                          → <span className="text-foreground">{labelize(t.toStage)}</span>
                          {isHuman && <span className="ml-1 text-blue-400">by {t.actorType}</span>}
                        </div>
                        {t.reason && <p className="text-[10px] italic text-muted-foreground mt-0.5">"{t.reason}"</p>}
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </ScrollArea>
        </div>
      )}
    </div>
  );
}