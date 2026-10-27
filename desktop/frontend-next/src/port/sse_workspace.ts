import type { ChangeDiff, StageRequest, WorkspaceBranches, WorkspaceChanges, WorkspaceGit } from "./port";
import type { DiscardRequest, GitCommitFiles, GitHistory, GitRepos, PushPlan, PushRequest, PushResult } from "./workspace";
import type { JobOutput, ToolOutputPage } from "./session";
import type { FleetRecoveryPlan, SubagentHandoff, SubagentRun, SubagentTranscript } from "./subagent";
import { SseFeedback } from "./sse_feedback";
import { host } from "./host";
import { HttpError } from "./http_error";

// The query a git read names its repository with: absent is the workspace's own.
function repoQuery(repo: string | undefined, ...rest: string[]): string {
  const parts = [...rest.filter(Boolean), repo ? `repo=${encodeURIComponent(repo)}` : ""].filter(Boolean);
  return parts.length ? `?${parts.join("&")}` : "";
}

// The workspace's tree facts: what it differs by and which branch git says it
// is on. The reads answer from the tree, so a frontend re-asks rather than
// deriving either from tool events.
export class SseWorkspace extends SseFeedback {
  opensFiles() { return host().opensFiles(); }

  async openFile(path: string) {
    const why = await host().openFile(this.base, path);
    if (why) throw new HttpError(0, why.error || "not opened", why, !!(why.code || why.error));
  }

  async changes(repo?: string) {
    const read = await this.get<WorkspaceChanges>("/changes" + repoQuery(repo));
    return { ...read, changes: read.changes ?? [] };
  }

  gitRepos() {
    return this.get<GitRepos>("/git/repos");
  }

  stageChanges(req: StageRequest) {
    return this.post("/changes/stage", req);
  }

  workspaceGit(repo?: string) {
    return this.get<WorkspaceGit>("/workspace/git" + repoQuery(repo));
  }

  workspaceGitLog(limit?: number, repo?: string) {
    return this.get<GitHistory>("/workspace/git/log" + repoQuery(repo, limit ? `limit=${limit}` : ""));
  }

  commitFiles(id: string, repo?: string) {
    return this.get<GitCommitFiles>("/git/commit" + repoQuery(repo, `id=${encodeURIComponent(id)}`));
  }

  commitDiff(id: string, path: string, repo?: string) {
    return this.get<ChangeDiff>("/git/commit/diff" + repoQuery(repo, `id=${encodeURIComponent(id)}&path=${encodeURIComponent(path)}`));
  }

  branches() {
    return this.get<WorkspaceBranches>("/workspace/branches");
  }

  switchBranch(name: string) {
    return this.post0<WorkspaceGit>("/workspace/branch/switch", { name });
  }

  changeDiff(path: string, repo?: string) {
    return this.get<ChangeDiff>("/changes/diff" + repoQuery(repo, `path=${encodeURIComponent(path)}`));
  }

  discardChange(req: DiscardRequest) {
    return this.post("/changes/discard", req);
  }

  pushPreview(repo?: string) {
    return this.get<PushPlan>("/git/push" + repoQuery(repo));
  }

  push(req: PushRequest) {
    return this.post0<PushResult>("/git/push", req);
  }

  stopToolCall(callId: string) {
    return this.post("/tool-calls/" + encodeURIComponent(callId) + "/cancel");
  }
  subagents() {
    return this.get<SubagentRun[]>("/subagents");
  }
  fleetRecoveryPlans(ref: string) {
    return this.get<FleetRecoveryPlan[]>("/subagents/" + encodeURIComponent(ref) + "/recovery");
  }
  requestFleetRecovery(ref: string, id: string, snapshot: string) {
    return this.post("/subagents/" + encodeURIComponent(ref) + "/recovery", { id, snapshot });
  }
  subagentTranscript(ref: string, after = 0, limit = 0) {
    const query = [after ? `after=${after}` : "", limit ? `limit=${limit}` : ""].filter(Boolean).join("&");
    return this.get<SubagentTranscript>("/subagents/" + encodeURIComponent(ref) + "/transcript" + (query ? "?" + query : ""));
  }
  steerSubagent(ref: string, text: string, context?: SubagentHandoff) {
    return this.post("/subagents/" + encodeURIComponent(ref) + (context ? "/handoff" : "/steer"), context ? { text, context } : { text });
  }
  cancelJob(jobId: string) {
    return this.post("/jobs/" + encodeURIComponent(jobId) + "/cancel");
  }
  jobOutput(jobId: string) {
    return this.get<JobOutput>("/jobs/" + encodeURIComponent(jobId) + "/output");
  }
  toolOutput(callId: string, offset = 0, limit?: number) {
    const q = new URLSearchParams({ offset: String(offset), ...(limit === undefined ? {} : { limit: String(limit) }) });
    return this.get<ToolOutputPage>("/history/tool/" + encodeURIComponent(callId) + "?" + q);
  }
}
