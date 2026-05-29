import { execFileSync } from "node:child_process";
import { existsSync, realpathSync } from "node:fs";
import { basename, dirname, isAbsolute, join, relative, resolve } from "node:path";
import type { ReasonixConfig } from "../config.js";
import { memoryEnabled } from "../memory/project.js";
import type { MemoryEntry } from "../memory/user.js";
import { MemoryStore, effectivePriority } from "../memory/user.js";
import type { EditBlock } from "./edit-blocks.js";

export interface AutoGitRollbackOptions {
  homeDir?: string;
  cfg?: ReasonixConfig;
}

export type AutoGitRollbackConfig = false | AutoGitRollbackOptions;

interface PrepareOptions {
  rootDir: string;
  toolName:
    | "edit_file"
    | "multi_edit"
    | "write_file"
    | "edit_blocks"
    | "delete_range"
    | "delete_symbol";
  absPaths: readonly string[];
  autoGitRollback?: AutoGitRollbackConfig;
}

function rejection(message: string, nextAction: string): string {
  return JSON.stringify({
    error: `auto-git-rollback: ${message}`,
    rejectedReason: "auto-git-rollback",
    nextAction,
  });
}

function cleanGitEnv(): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.startsWith("GIT_")) delete env[key];
  }
  return env;
}

function runGitRaw(cwd: string, args: readonly string[]): string {
  return execFileSync("git", ["-C", cwd, ...args], {
    encoding: "utf8",
    env: cleanGitEnv(),
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function runGit(cwd: string, args: readonly string[]): string {
  return runGitRaw(cwd, args).trim();
}

function tryGit(cwd: string, args: readonly string[]): string | null {
  try {
    return runGit(cwd, args);
  } catch {
    return null;
  }
}

function tryGitRaw(cwd: string, args: readonly string[]): string | null {
  try {
    return runGitRaw(cwd, args);
  } catch {
    return null;
  }
}

function realpathNative(path: string): string {
  return realpathSync.native(resolve(path));
}

function gitTopLevel(rootDir: string): string | null {
  const out = tryGit(rootDir, ["rev-parse", "--show-toplevel"]);
  return out ? realpathNative(out) : null;
}

function isAutoGitRollbackEntry(entry: MemoryEntry, cfg: ReasonixConfig | undefined): boolean {
  if (effectivePriority(entry, cfg) !== "high") return false;
  const text = [entry.name, entry.type, entry.description, entry.body].join("\n").toLowerCase();
  return text.includes("auto-git-rollback");
}

function autoGitRollbackActive(
  rootDir: string,
  autoGitRollback: AutoGitRollbackConfig | undefined,
): boolean {
  if (autoGitRollback === false || !memoryEnabled()) return false;
  const opts = autoGitRollback ?? {};
  const top = gitTopLevel(rootDir);
  if (!top) return false;
  const store = new MemoryStore({ homeDir: opts.homeDir, projectRoot: rootDir });
  return store.list().some((entry) => isAutoGitRollbackEntry(entry, opts.cfg));
}

function pathIsUnder(child: string, parent: string): boolean {
  const rel = relative(parent, child);
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

function unique<T>(items: readonly T[]): T[] {
  return [...new Set(items)];
}

function targetExistsOrIsTracked(topLevel: string, relPath: string): boolean {
  if (existsSync(join(topLevel, relPath))) return true;
  return tryGit(topLevel, ["ls-files", "--error-unmatch", "--", relPath]) !== null;
}

function realpathForTarget(abs: string): string {
  const resolved = resolve(abs);
  if (existsSync(resolved)) return realpathNative(resolved);
  const missing: string[] = [basename(resolved)];
  let parent = dirname(resolved);
  while (!existsSync(parent)) {
    const next = dirname(parent);
    if (next === parent) return resolved;
    missing.unshift(basename(parent));
    parent = next;
  }
  return join(realpathNative(parent), ...missing);
}

function commitMessage(toolName: string, relPaths: readonly string[]): string {
  const shown = relPaths.slice(0, 3).join(", ");
  const suffix = relPaths.length > 3 ? ` +${relPaths.length - 3}` : "";
  return `pre-edit: ${toolName} ${shown}${suffix}`;
}

function parseNameList(out: string): string[] {
  return out
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function normalizeGitRelPath(path: string): string {
  return path.replaceAll("\\", "/");
}

function parseStatusPath(out: string | null): string | null {
  if (!out) return null;
  for (const entry of out.split("\0")) {
    if (entry.length <= 3) continue;
    const path = entry.slice(3);
    if (path.length > 0) return normalizeGitRelPath(path);
  }
  return null;
}

function gitRelPathForTarget(topLevel: string, absPath: string): string | null {
  const tracked = parseNameList(tryGit(topLevel, ["ls-files", "--full-name", "--", absPath]) ?? "");
  if (tracked.length > 0) return normalizeGitRelPath(tracked[0]!);
  return parseStatusPath(tryGitRaw(topLevel, ["status", "--porcelain=v1", "-z", "--", absPath]));
}

function relPathForTarget(topLevel: string, absPath: string): string | null {
  const resolved = realpathForTarget(absPath);
  if (pathIsUnder(resolved, topLevel)) {
    return normalizeGitRelPath(relative(topLevel, resolved));
  }

  // Windows runners can expose the same Temp directory through a short 8.3
  // alias (for example RUNNER~1) while Git reports the long worktree path.
  // When that happens, the lexical realpath comparison above cannot prove
  // containment, but Git can still map the absolute pathspec to a repo path.
  return gitRelPathForTarget(topLevel, absPath) ?? gitRelPathForTarget(topLevel, resolved);
}

export function prepareAutoGitRollback(opts: PrepareOptions): string | null {
  const active = autoGitRollbackActive(opts.rootDir, opts.autoGitRollback);
  if (!active) return null;

  const topLevel = gitTopLevel(opts.rootDir);
  if (!topLevel) return null;
  if (!existsSync(join(topLevel, ".gitignore"))) {
    return rejection(
      "refusing to mutate before a repository .gitignore is present",
      "create_or_confirm_gitignore",
    );
  }

  const rawRelPaths: string[] = [];
  for (const abs of opts.absPaths) {
    const relPath = relPathForTarget(topLevel, abs);
    if (relPath === null) {
      return rejection(
        `target path escapes git worktree: ${realpathForTarget(abs)}`,
        "choose_in_repo_target",
      );
    }
    rawRelPaths.push(relPath);
  }
  const relPaths = unique(rawRelPaths);

  if (relPaths.length === 0) return null;

  const stagedBefore = parseNameList(runGit(topLevel, ["diff", "--cached", "--name-only"]));
  const targetSet = new Set(relPaths);
  const unrelatedStaged = stagedBefore.filter((p) => !targetSet.has(p));
  if (unrelatedStaged.length > 0) {
    return rejection(
      `refusing to commit unrelated staged changes: ${unrelatedStaged.join(", ")}`,
      "commit_or_unstage_unrelated_changes",
    );
  }

  const stageable = relPaths.filter((p) => targetExistsOrIsTracked(topLevel, p));
  if (stageable.length > 0) {
    try {
      runGit(topLevel, ["add", "--", ...stageable]);
    } catch (err) {
      return rejection((err as Error).message, "fix_git_add_failure");
    }
  }

  const hasStagedChanges = (() => {
    try {
      runGit(topLevel, ["diff", "--cached", "--quiet"]);
      return false;
    } catch {
      return true;
    }
  })();
  if (hasStagedChanges) {
    try {
      runGit(topLevel, ["commit", "-m", commitMessage(opts.toolName, relPaths), "--quiet"]);
    } catch (err) {
      return rejection((err as Error).message, "fix_git_commit_failure");
    }
  }

  const status = runGit(topLevel, ["status", "--porcelain"]);
  if (status.length > 0) {
    return rejection(
      "refusing to mutate while the worktree still has uncommitted changes after the pre-edit checkpoint",
      "commit_stash_or_clean_worktree",
    );
  }

  return null;
}

function looksLikeAbsoluteSystemPath(rawPath: string): boolean {
  return /^\/(?:home|Users|etc|var|opt|tmp|usr|mnt|Library|Volumes|proc|sys|dev|run|srv|media|Applications|System|root|boot|private)(?:[/\\]|$)/.test(
    rawPath,
  );
}

function resolveBlockPath(rootDir: string, rawPath: string): string {
  if (/^[A-Za-z]:[\\/]/.test(rawPath) || looksLikeAbsoluteSystemPath(rawPath)) {
    return resolve(rawPath);
  }
  let rooted = rawPath;
  while (rooted.startsWith("/") || rooted.startsWith("\\")) {
    rooted = rooted.slice(1);
  }
  return resolve(rootDir, rooted || ".");
}

export function prepareAutoGitRollbackForEditBlocks(
  rootDir: string,
  blocks: readonly EditBlock[],
  autoGitRollback?: AutoGitRollbackConfig,
): string | null {
  return prepareAutoGitRollback({
    rootDir,
    toolName: "edit_blocks",
    absPaths: blocks.map((block) => resolveBlockPath(rootDir, block.path)),
    autoGitRollback,
  });
}

export function formatAutoGitRollbackRejection(result: string): string {
  try {
    const parsed = JSON.parse(result) as { error?: unknown };
    if (typeof parsed.error === "string") return parsed.error;
  } catch {
    /* keep original text */
  }
  return result;
}
