import type { ChangeDiff, WorkspaceBranches, WorkspaceChanges, WorkspaceGit } from "./port";
import { SseFeedback } from "./sse_feedback";

// The workspace's tree facts: what it differs by and which branch git says it
// is on. The reads answer from the tree, so a frontend re-asks rather than
// deriving either from tool events.
export class SseWorkspace extends SseFeedback {
  changes() {
    return this.get<WorkspaceChanges>("/changes");
  }

  workspaceGit() {
    return this.get<WorkspaceGit>("/workspace/git");
  }

  branches() {
    return this.get<WorkspaceBranches>("/workspace/branches");
  }

  switchBranch(name: string) {
    return this.post0<WorkspaceGit>("/workspace/branch/switch", { name });
  }

  changeDiff(path: string) {
    return this.get<ChangeDiff>(`/changes/diff?path=${encodeURIComponent(path)}`);
  }
}
