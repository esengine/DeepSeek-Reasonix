import { execFileSync } from "node:child_process";
import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MemoryStore } from "../src/memory/user.js";
import { ToolRegistry } from "../src/tools.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { ReadTracker } from "../src/tools/read-tracker.js";

function cleanGitEnv(overrides: NodeJS.ProcessEnv = {}): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.startsWith("GIT_")) delete env[key];
  }
  return { ...env, ...overrides };
}

function git(cwd: string, args: string[]): string {
  return execFileSync("git", args, {
    cwd,
    encoding: "utf8",
    env: cleanGitEnv({
      GIT_AUTHOR_NAME: "Reasonix Test",
      GIT_AUTHOR_EMAIL: "reasonix@example.test",
      GIT_COMMITTER_NAME: "Reasonix Test",
      GIT_COMMITTER_EMAIL: "reasonix@example.test",
    }),
  }).trim();
}

function enableAutoGitRollback(home: string, root: string): void {
  const store = new MemoryStore({ homeDir: home, projectRoot: root });
  store.write({
    name: "auto-git-rollback",
    type: "workflow",
    scope: "global",
    description: "run git add and git commit before edit_file/multi_edit/write_file",
    body: [
      "Before edit_file, multi_edit, or write_file, create a git rollback point.",
      "Run git add for the target files, then git diff --cached --quiet || git commit.",
      "Refuse edits unless the worktree is clean after that checkpoint.",
    ].join("\n"),
    priority: "high",
  });
}

function writePlainGitWorkflowMemory(home: string, root: string): void {
  const store = new MemoryStore({ homeDir: home, projectRoot: root });
  store.write({
    name: "normal-git-workflow",
    type: "workflow",
    scope: "global",
    description: "documents git add and git commit around edit_file usage",
    body: [
      "Before larger changes, inspect the diff and decide what to stage.",
      "Use git add and git commit intentionally after edit_file, multi_edit, or write_file changes.",
    ].join("\n"),
    priority: "high",
  });
}

function restoreProcessEnv(name: string, value: string | undefined): void {
  if (value === undefined) {
    Reflect.deleteProperty(process.env, name);
    return;
  }
  process.env[name] = value;
}

describe("auto-git-rollback memory guard", () => {
  let root: string;
  let home: string;
  let tools: ToolRegistry;
  let readTracker: ReadTracker;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-auto-git-root-"));
    home = await mkdtemp(join(tmpdir(), "reasonix-auto-git-home-"));
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root, autoGitRollback: { homeDir: home } });
    readTracker = new ReadTracker();

    git(root, ["init", "-q"]);
    git(root, ["config", "user.name", "Reasonix Test"]);
    git(root, ["config", "user.email", "reasonix@example.test"]);
    await fs.writeFile(join(root, ".gitignore"), "node_modules/\n", "utf8");
    await fs.writeFile(join(root, "tracked.txt"), "baseline\n", "utf8");
    await fs.writeFile(join(root, "other.txt"), "other baseline\n", "utf8");
    git(root, ["add", ".gitignore", "tracked.txt", "other.txt"]);
    git(root, ["commit", "-m", "test: initial commit", "-q"]);
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
    await rm(home, { recursive: true, force: true });
  });

  it("creates a pre-edit git checkpoint for dirty target files before edit_file writes", async () => {
    enableAutoGitRollback(home, root);
    await fs.writeFile(join(root, "tracked.txt"), "dirty-before-edit\n", "utf8");

    await tools.dispatch("read_file", { path: "tracked.txt" }, { readTracker });
    const out = await tools.dispatch(
      "edit_file",
      { path: "tracked.txt", search: "dirty-before-edit", replace: "after-edit" },
      { readTracker },
    );

    expect(out).toMatch(/edited tracked\.txt/);
    expect(git(root, ["show", "HEAD:tracked.txt"])).toBe("dirty-before-edit");
    expect(git(root, ["log", "-1", "--format=%s"])).toMatch(/pre-edit: edit_file tracked\.txt/);
    expect(await fs.readFile(join(root, "tracked.txt"), "utf8")).toBe("after-edit\n");
  });

  it("does not activate from a high-priority memory that only describes ordinary git workflow", async () => {
    writePlainGitWorkflowMemory(home, root);
    await fs.writeFile(join(root, "tracked.txt"), "dirty-before-edit\n", "utf8");

    await tools.dispatch("read_file", { path: "tracked.txt" }, { readTracker });
    const out = await tools.dispatch(
      "edit_file",
      { path: "tracked.txt", search: "dirty-before-edit", replace: "after-edit" },
      { readTracker },
    );

    expect(out).toMatch(/edited tracked\.txt/);
    expect(git(root, ["show", "HEAD:tracked.txt"])).toBe("baseline");
    expect(git(root, ["log", "--format=%s"])).not.toContain("pre-edit:");
    expect(await fs.readFile(join(root, "tracked.txt"), "utf8")).toBe("after-edit\n");
  });

  it.each([
    {
      toolName: "multi_edit",
      args: {
        edits: [{ path: "tracked.txt", search: "dirty-before-edit", replace: "after-edit" }],
      },
    },
    {
      toolName: "write_file",
      args: { path: "tracked.txt", content: "after-edit\n" },
    },
  ])("creates the same pre-edit checkpoint before $toolName writes", async ({ toolName, args }) => {
    enableAutoGitRollback(home, root);
    await fs.writeFile(join(root, "tracked.txt"), "dirty-before-edit\n", "utf8");

    await tools.dispatch("read_file", { path: "tracked.txt" }, { readTracker });
    const out = await tools.dispatch(toolName, args, { readTracker });

    expect(out).toMatch(toolName === "write_file" ? /wrote \d+ chars/ : /applied 1 edit/);
    expect(git(root, ["show", "HEAD:tracked.txt"])).toBe("dirty-before-edit");
    expect(git(root, ["log", "-1", "--format=%s"])).toMatch(
      new RegExp(`pre-edit: ${toolName} tracked\\.txt`),
    );
    expect(await fs.readFile(join(root, "tracked.txt"), "utf8")).toBe("after-edit\n");
  });

  it("refuses to write when unrelated worktree changes remain after checkpointing targets", async () => {
    enableAutoGitRollback(home, root);
    await fs.writeFile(join(root, "other.txt"), "unrelated dirty\n", "utf8");

    await tools.dispatch("read_file", { path: "tracked.txt" }, { readTracker });
    const out = await tools.dispatch(
      "edit_file",
      { path: "tracked.txt", search: "baseline", replace: "after-edit" },
      { readTracker },
    );

    expect(JSON.parse(out)).toMatchObject({
      rejectedReason: "auto-git-rollback",
      nextAction: "commit_stash_or_clean_worktree",
    });
    expect(await fs.readFile(join(root, "tracked.txt"), "utf8")).toBe("baseline\n");
  });

  it("ignores ambient Git hook environment variables when checkpointing", async () => {
    const outerRoot = await mkdtemp(join(tmpdir(), "reasonix-auto-git-outer-"));
    git(outerRoot, ["init", "-q"]);
    const previousGitDir = process.env.GIT_DIR;
    const previousGitWorkTree = process.env.GIT_WORK_TREE;
    process.env.GIT_DIR = join(outerRoot, ".git");
    process.env.GIT_WORK_TREE = outerRoot;

    try {
      enableAutoGitRollback(home, root);
      await fs.writeFile(join(root, "tracked.txt"), "dirty-before-edit\n", "utf8");

      await tools.dispatch("read_file", { path: "tracked.txt" }, { readTracker });
      const out = await tools.dispatch(
        "edit_file",
        { path: "tracked.txt", search: "dirty-before-edit", replace: "after-edit" },
        { readTracker },
      );

      expect(out).toMatch(/edited tracked\.txt/);
      expect(git(root, ["show", "HEAD:tracked.txt"])).toBe("dirty-before-edit");
      expect(git(root, ["log", "-1", "--format=%s"])).toMatch(/pre-edit: edit_file tracked\.txt/);
    } finally {
      restoreProcessEnv("GIT_DIR", previousGitDir);
      restoreProcessEnv("GIT_WORK_TREE", previousGitWorkTree);
      await rm(outerRoot, { recursive: true, force: true });
    }
  });
});
