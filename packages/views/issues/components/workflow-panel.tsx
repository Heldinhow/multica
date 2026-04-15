"use client";

import { useCallback, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
import type { WorkflowApproval, WorkflowArtifact, WorkflowRun, WorkflowStep, WorkflowTaskAttempt } from "@multica/core/types";
import { workflowKeys, issueWorkflowsOptions } from "@multica/core/workflows/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { ChevronRight, GitBranch, PlayCircle, CheckCircle2, XCircle, Loader2, ShieldCheck, ShieldX, Clock3 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { toast } from "sonner";

interface WorkflowPanelProps {
  issueId: string;
}

const statusTone: Record<string, string> = {
  planning: "bg-info/10 text-info border-info/20",
  awaiting_plan_approval: "bg-warning/10 text-warning border-warning/20",
  executing: "bg-info/10 text-info border-info/20",
  awaiting_handoff_approval: "bg-warning/10 text-warning border-warning/20",
  blocked: "bg-destructive/10 text-destructive border-destructive/20",
  completed: "bg-success/10 text-success border-success/20",
  cancelled: "bg-muted text-muted-foreground border-border",
};

export function WorkflowPanel({ issueId }: WorkflowPanelProps) {
  const qc = useQueryClient();
  const { getActorName } = useActorName();
  const { data, isLoading } = useQuery(issueWorkflowsOptions(issueId));
  const runs: WorkflowRun[] = data ?? [];
  const [creating, setCreating] = useState(false);
  const latestRun: WorkflowRun | null = runs[0] ?? null;
  const latestSteps = latestRun?.steps ?? [];
  const latestApprovals = latestRun?.approvals ?? [];
  const latestArtifacts = latestRun?.artifacts ?? [];

  const refresh = useCallback(() => {
    qc.invalidateQueries({ queryKey: workflowKeys.byIssue(issueId) });
    if (latestRun?.id) {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(latestRun.id) });
    }
  }, [qc, issueId, latestRun?.id]);

  useWSEvent("workflow:run_updated", useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id === issueId) refresh();
  }, [issueId, refresh]));
  useWSEvent("workflow:step_updated", useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id === issueId) refresh();
  }, [issueId, refresh]));
  useWSEvent("workflow:approval_requested", useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id === issueId) refresh();
  }, [issueId, refresh]));
  useWSEvent("workflow:event_created", useCallback((payload: unknown) => {
    const p = payload as { issue_id?: string };
    if (p.issue_id === issueId) refresh();
  }, [issueId, refresh]));

  const pendingApprovals = useMemo(
    () => latestApprovals.filter((approval: WorkflowApproval) => approval.status === "pending"),
    [latestApprovals],
  );

  const createWorkflow = useCallback(async () => {
    if (creating) return;
    setCreating(true);
    try {
      await api.createWorkflow(issueId);
      await qc.invalidateQueries({ queryKey: workflowKeys.byIssue(issueId) });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to create workflow");
    } finally {
      setCreating(false);
    }
  }, [creating, issueId, qc]);

  const resolveApproval = useCallback(async (runId: string, approvalId: string, action: "approve" | "reject") => {
    try {
      if (action === "approve") {
        await api.approveWorkflowApproval(runId, approvalId);
      } else {
        await api.rejectWorkflowApproval(runId, approvalId);
      }
      refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : `Failed to ${action} approval`);
    }
  }, [refresh]);

  const cancelRun = useCallback(async (runId: string) => {
    try {
      await api.cancelWorkflowRun(runId);
      refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to cancel workflow");
    }
  }, [refresh]);

  return (
    <div className="rounded-xl border bg-card/70 p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Workflow</p>
          <h3 className="text-sm font-semibold text-foreground">Multi-agent execution</h3>
        </div>
        <Button size="sm" onClick={createWorkflow} disabled={creating || (!!latestRun && !["completed", "cancelled"].includes(latestRun.status))}>
          {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <PlayCircle className="mr-2 h-4 w-4" />}
          Start workflow
        </Button>
      </div>

      {isLoading ? (
        <div className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading workflow state...
        </div>
      ) : latestRun ? (
        <div className="mt-4 space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className={cn("capitalize", statusTone[latestRun.status] ?? "")}>
              {latestRun.status.replaceAll("_", " ")}
            </Badge>
            <Badge variant="outline" className="capitalize">
              phase {latestRun.phase.replaceAll("_", " ")}
            </Badge>
            <Badge variant="outline">
              budget {latestRun.token_budget.toLocaleString()}
            </Badge>
            <Badge variant="outline">
              replan {latestRun.replan_count}/{latestRun.max_replans}
            </Badge>
            {latestRun.base_branch ? (
              <Badge variant="outline">
                <GitBranch className="mr-1 h-3 w-3" />
                {latestRun.base_branch}
              </Badge>
            ) : null}
          </div>

          {pendingApprovals.length > 0 ? (
            <div className="rounded-lg border border-warning/20 bg-warning/5 p-3">
              <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                <Clock3 className="h-4 w-4 text-warning" />
                Pending approvals
              </div>
              <div className="space-y-2">
                {pendingApprovals.map((approval: WorkflowApproval) => (
                  <div key={approval.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border/70 bg-background/80 px-3 py-2">
                    <Badge variant="outline" className="capitalize">{approval.scope}</Badge>
                    <span className="text-sm text-muted-foreground">{approval.comment || "Review required before the workflow can continue."}</span>
                    <div className="ml-auto flex items-center gap-2">
                      <Button size="sm" variant="outline" onClick={() => resolveApproval(latestRun.id, approval.id, "reject")}>
                        <ShieldX className="mr-2 h-4 w-4" />
                        Reject
                      </Button>
                      <Button size="sm" onClick={() => resolveApproval(latestRun.id, approval.id, "approve")}>
                        <ShieldCheck className="mr-2 h-4 w-4" />
                        Approve
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          <div className="space-y-2">
            {latestSteps.map((step: WorkflowStep) => {
              const assignedName = step.assigned_agent_id ? getActorName("agent", step.assigned_agent_id) : "Unassigned";
              return (
                <Collapsible key={step.id} defaultOpen={step.status === "running" || step.status === "awaiting_approval"}>
                  <div className="rounded-lg border border-border/70 bg-background/80">
                    <CollapsibleTrigger className="flex w-full items-center gap-3 px-3 py-2 text-left">
                      <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground transition-transform data-[state=open]:rotate-90" />
                      <StepStatusIcon status={step.status} />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-medium text-foreground">{step.title}</span>
                          <Badge variant="outline" className="capitalize">{step.role}</Badge>
                          <Badge variant="outline" className="capitalize">{step.write_scope.replaceAll("_", " ")}</Badge>
                          {step.repo_target ? <Badge variant="outline">{step.repo_target}</Badge> : null}
                        </div>
                        <p className="truncate text-xs text-muted-foreground">{step.objective || "No objective provided."}</p>
                      </div>
                      <div className="text-right text-xs text-muted-foreground">
                        <div>{assignedName}</div>
                        <div>{step.attempt_count} attempts</div>
                      </div>
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <div className="space-y-3 border-t border-border/60 px-4 py-3">
                        <div className="text-sm text-muted-foreground">{step.objective || "No objective provided."}</div>
                        {step.attempts.length > 0 ? (
                          <div className="space-y-1">
                            <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">Attempts</div>
                            {step.attempts.map((attempt: WorkflowTaskAttempt) => (
                              <div key={attempt.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border/60 px-3 py-2 text-xs text-muted-foreground">
                                <Badge variant="outline">#{attempt.attempt_no}</Badge>
                                <span className="capitalize">{attempt.status}</span>
                                <span>{getActorName("agent", attempt.agent_id)}</span>
                                {attempt.error ? <span className="text-destructive">{attempt.error}</span> : null}
                              </div>
                            ))}
                          </div>
                        ) : null}
                        {step.metadata.expected_artifacts && Array.isArray(step.metadata.expected_artifacts) ? (
                          <div className="flex flex-wrap gap-2">
                            {(step.metadata.expected_artifacts as string[]).map((artifact) => (
                              <Badge key={artifact} variant="outline">{artifact}</Badge>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    </CollapsibleContent>
                  </div>
                </Collapsible>
              );
            })}
          </div>

          {latestArtifacts.length > 0 ? (
            <div className="space-y-2">
              <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">Artifacts</div>
              <div className="grid gap-2 md:grid-cols-2">
                {latestArtifacts.map((artifact: WorkflowArtifact) => (
                  <div key={artifact.id} className="rounded-lg border border-border/70 bg-background/80 p-3">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{artifact.artifact_type}</Badge>
                      <span className="text-sm font-medium text-foreground">{artifact.summary || "Artifact"}</span>
                    </div>
                    {"output" in artifact.content ? (
                      <p className="mt-2 line-clamp-4 text-sm text-muted-foreground">{String(artifact.content.output ?? "")}</p>
                    ) : artifact.content.plan_summary ? (
                      <p className="mt-2 line-clamp-4 text-sm text-muted-foreground">{String(artifact.content.plan_summary)}</p>
                    ) : null}
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          {latestRun.status !== "completed" && latestRun.status !== "cancelled" ? (
            <div className="flex justify-end">
              <Button size="sm" variant="outline" onClick={() => cancelRun(latestRun.id)}>
                Cancel workflow
              </Button>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="mt-4 rounded-lg border border-dashed border-border/80 bg-muted/20 p-4 text-sm text-muted-foreground">
          No workflow started for this issue yet. Starting one will create a planner step, request human approval for the generated plan, and then coordinate coder/reviewer/tester handoffs inside the issue.
        </div>
      )}
    </div>
  );
}

function StepStatusIcon({ status }: { status: WorkflowRun["steps"][number]["status"] }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />;
    case "failed":
    case "blocked":
    case "rejected":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
    default:
      return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-info" />;
  }
}
