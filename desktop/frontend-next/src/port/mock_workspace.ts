import { reason } from "../i18n/kernel";
import { HttpError } from "./http_error";
import type { KeptText } from "./attachment";
import type { ChangeDiff, GitHistory, StageRequest, WorkspaceChange, WorkspaceChanges, WorkspaceGit, WorkspaceBranches } from "./workspace";
import { MockMemoryHold } from "./mock_memory_hold";

export function mockWorkspaces(root: string): import("./workspace").WorkspaceInfo {
  return { current: root, canSwitch: true, canIsolate: true, recents: [
    { path: "~/projects/reasonix-site", name: "reasonix-site" },
    { path: "~/work/notes", name: "notes" },
  ] };
}

export function mockWorktreeSources(root: string, branch: string): import("./workspace").WorktreeSources {
  return { available: branch !== "reasonix/delivery-demo", sourceRoot: root, sources: [
    { ref: "refs/heads/main", name: "main", remote: false, current: branch === "main" },
    { ref: "refs/heads/studio", name: "studio", remote: false, current: branch === "studio" },
    { ref: "refs/remotes/origin/main", name: "origin/main", remote: true, current: false },
  ] };
}

export class MockWorkspace extends MockMemoryHold {
  async workspaceFile(path: string, _root?: string): Promise<import("./workspace").WorkspaceFile> { return { path, content: `// ${path}\n`, revision: "fixture" }; }
  async workspacePreviewInfo(ref: import("./workspace").FileRef): Promise<import("./workspace").PreviewInfo> {
    const file = await this.workspaceFile(ref.path, ref.root);
    const { previewKind } = await import("../ui/document/registry");
    return { ref, kind: previewKind(ref.path), size: new TextEncoder().encode(file.content).length, version: file.revision, readOnly: !!ref.root, limit: 2 << 20 };
  }
  async workspacePreviewContent(ref: import("./workspace").FileRef) {
    const file = await this.workspaceFile(ref.path, ref.root);
    return { data: new TextEncoder().encode(file.content).buffer, version: file.revision };
  }
  workspaceImageURL(path: string) { return `/fixture/${path}`; }
  opensFiles() { return false; }
  async openFile() {}

  async keepText(text: string): Promise<KeptText> {
    const path = ".reasonix/attachments/mock.txt";
    return { path, pointer: `[pasted text kept out of this message: ${text.split("\n").length} lines]\nSaved: ${path}\nRead it with read_file.\n` };
  }

  // Held in a field so a switch is visible to the next read, as the kernel's endpoints keep.
  branchState: WorkspaceGit = { repo: true, name: "reasonix", branch: "main", detached: false, added: 2, removed: 1, untracked: 3 };

  async workspaceGit(): Promise<WorkspaceGit> {
    return { ...this.branchState };
  }

  // A small history with a side branch that merges back, so the graph has a
  // fork, a merge and two lanes to draw.
  async workspaceGitLog(): Promise<GitHistory> {
    const now = Math.floor(Date.now() / 1000);
    const c = (id: string, parents: string[], subject: string, mins: number, refs: string[] = [], author = "ada") =>
      ({ id, parents, subject, author, at: now - mins * 60, refs });
    return {
      repo: true, truncated: false,
      commits: [
        c("a1", ["a2"], "feat(studio): show the commit graph", 4, ["HEAD -> main", "origin/main"]),
        c("a2", ["a3", "b2"], "Merge branch 'retry-backoff'", 38),
        c("b2", ["b1"], "fix(provider): honour Retry-After", 71, ["retry-backoff"], "grace"),
        c("a3", ["a4"], "docs: describe the wire log", 95),
        c("b1", ["a4"], "test(provider): cover 429 backoff", 130, [], "grace"),
        c("a4", ["a5"], "refactor(config): split the loader", 260, ["tag: v2.38.0"]),
        c("a5", [], "chore: initial import", 1440),
      ],
    };
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
  // Held in a field so staging is visible to the next read, as the index is.
  changeList: WorkspaceChange[] = [
    { path: "internal/provider/retry.go", status: "M", unstaged: true, insertions: 14, deletions: 3 },
    { path: "internal/config/credentials.go", status: "M", staged: true, insertions: 6, deletions: 2 },
    { path: "internal/provider/openai/streaming/chunk_decoder.go", status: "A", staged: true, unstaged: true },
    { path: "docs/notes.md", status: "??", unstaged: true },
  ];

  async changes(repo?: string): Promise<WorkspaceChanges> {
    return { repo: true, changes: (repo ? this.repoChanges[repo] ?? [] : this.changeList).map((c) => ({ ...c })) };
  }

  async stageChanges(req: StageRequest): Promise<void> {
    const named = new Set(req.paths ?? []);
    const mark = (list: WorkspaceChange[]) =>
      list.map((c) => (req.all || named.has(c.path) ? { ...c, staged: !req.unstage, unstaged: !!req.unstage } : c));
    if (req.repo) this.repoChanges[req.repo] = mark(this.repoChanges[req.repo] ?? []);
    else this.changeList = mark(this.changeList);
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

export class MockPreparations {
  private items = new Map<string, import("./workspace").WorktreePreparation>();
  async prepare(id: string, selection: import("./workspace").WorktreeSelection, create: () => Promise<string>) {
    const found = this.items.get(id);
    if (found) return found;
    const value: import("./workspace").WorktreePreparation = { id, phase: "validating", sourceRoot: selection.sourceRoot, ref: selection.ref, startedAt: Date.now() };
    try {
      value.workspaceRoot = await create();
      value.phase = "ready";
      value.branch = "reasonix/demo";
    } catch (error) {
      value.phase = "failed";
      value.code = "workspace.worktree_failed";
      value.detail = reason(error);
    }
    this.items.set(id, value);
    return value;
  }
  async read(id: string) {
    const value = this.items.get(id);
    if (!value) throw new Error("unknown worktree operation");
    return value;
  }
}
