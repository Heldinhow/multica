import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const workflowKeys = {
  byIssue: (issueId: string) => ["workflows", "issue", issueId] as const,
  detail: (runId: string) => ["workflows", "detail", runId] as const,
};

export function issueWorkflowsOptions(issueId: string) {
  return queryOptions({
    queryKey: workflowKeys.byIssue(issueId),
    queryFn: () => api.listIssueWorkflows(issueId),
  });
}

export function workflowRunOptions(runId: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(runId),
    queryFn: () => api.getWorkflowRun(runId),
  });
}
