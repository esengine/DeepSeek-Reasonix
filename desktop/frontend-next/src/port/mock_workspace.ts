import { HttpError } from "./http_error";
import type { ChangeDiff, WorkspaceChanges, WorkspaceGit, WorkspaceBranches } from "./workspace";
import { MockFeedback } from "./mock_feedback";

export class MockWorkspace extends MockFeedback {
  // Held in a field so a switch is visible to the next read, as the kernel's endpoints keep.
  branchState: WorkspaceGit = { repo: true, name: "reasonix", branch: "main", detached: false, added: 2, removed: 1, untracked: 3 };

  async workspaceGit(): Promise<WorkspaceGit> {
    return { ...this.branchState };
  }

  async branches(): Promise<WorkspaceBranches> {
    return {
      repo: this.branchState.repo,
      branches: ["main", "studio"].map((name) => ({ name, current: this.branchState.branch === name })),
    };
  }

  async switchBranch(name: string): Promise<WorkspaceGit> {
    if (!["main", "studio"].includes(name)) {
      throw new HttpError(409, "/workspace/branch/switch: no such branch", {
        code: "branch.unknown", error: "no local branch with that name",
      });
    }
    this.branchState = { ...this.branchState, branch: name, detached: false };
    return { ...this.branchState };
  }

  // The scripted transcript's tree facts, without a repository behind the fixture.
  async changes(): Promise<WorkspaceChanges> {
    return {
      repo: true,
      changes: [
        { path: "internal/provider/retry.go", status: "M" },
        { path: "internal/config/credentials.go", status: "M" },
        { path: "internal/provider/openai/streaming/chunk_decoder.go", status: "A" },
      ],
    };
  }

  // Scripted, because there is no tree to read: enough of a diff for the
  // preview to be worked on without a kernel behind the window.
  async changeDiff(path: string): Promise<ChangeDiff> {
    return {
      path,
      diff: [
        `--- a/${path}`,
        `+++ b/${path}`,
        "@@ -1,6 +1,7 @@",
        " package permission",
        " ",
        "-func allows(cmd string) bool {",
        "-	return strings.Contains(cmd, \"rm -rf\")",
        "+func allows(cmd string) bool {",
        "+	ast, err := shellparse.Parse(cmd)",
        "+	if err != nil {",
        "+		return false",
        "+	}",
        " }",
      ].join("\n"),
      truncated: false,
    };
  }

}
