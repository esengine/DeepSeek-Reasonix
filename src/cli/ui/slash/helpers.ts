import { spawnSync } from "node:child_process";
import type { MemoryScope, MemoryStore } from "../../../memory/user.js";
import type { SlashResult } from "./types.js";

/** Bare names try project scope first (more specific) before falling back to global. */
export function resolveMemoryTarget(
  store: MemoryStore,
  raw: string,
): { scope: MemoryScope; name: string } | null {
  const slash = raw.indexOf("/");
  if (slash > 0) {
    const scopeRaw = raw.slice(0, slash).toLowerCase();
    const name = raw.slice(slash + 1);
    if (scopeRaw !== "global" && scopeRaw !== "project") return null;
    const scope = scopeRaw as MemoryScope;
    if (scope === "project" && !store.hasProjectScope()) return null;
    return { scope, name };
  }
  for (const scope of ["project", "global"] as MemoryScope[]) {
    if (scope === "project" && !store.hasProjectScope()) continue;
    try {
      store.read(scope, raw);
      return { scope, name: raw };
    } catch {
      /* next scope */
    }
  }
  return null;
}

export function appendSection(
  lines: string[],
  label: string,
  section:
    | { supported: true; items: Array<{ name: string }> }
    | { supported: false; reason: string }
    | undefined,
): void {
  if (!section || !section.supported) {
    lines.push(
      `  ${label.trim()}    ${section?.supported === false ? "(not supported)" : "(none)"}`,
    );
    return;
  }
  const names = section.items.map((i) => i.name);
  if (names.length === 0) {
    lines.push(`  ${label.trim()}    (none)`);
    return;
  }
  const head = names.slice(0, 5).join(", ");
  const more = names.length > 5 ? ` +${names.length - 5} more` : "";
  lines.push(`  ${label.trim()}    ${names.length}  [${head}${more}]`);
}

/** Binary-K to match DeepSeek docs; do NOT reuse for non-token counts. */
export function compactNum(n: number): string {
  if (n < 1024) return String(n);
  const k = n / 1024;
  return k >= 100 ? `${Math.round(k)}K` : `${k.toFixed(1)}K`;
}

export function stripOuterQuotes(s: string): string {
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"')) {
    return s.slice(1, -1);
  }
  return s;
}

export function runGitCommit(rootDir: string, message: string): SlashResult {
  const add = spawnSync("git", ["add", "-A"], { cwd: rootDir, encoding: "utf8" });
  if (add.error || add.status !== 0) {
    return { info: `git add failed (${add.status ?? "?"}):\n${gitTail(add)}` };
  }
  const commit = spawnSync("git", ["commit", "-m", message], {
    cwd: rootDir,
    encoding: "utf8",
  });
  if (commit.error || commit.status !== 0) {
    return { info: `git commit failed (${commit.status ?? "?"}):\n${gitTail(commit)}` };
  }
  const firstLine = (commit.stdout || "").split(/\r?\n/)[0] ?? "";
  return { info: `▸ committed: ${message}${firstLine ? `\n  ${firstLine}` : ""}` };
}

/** On Windows or missing cwd, stderr/stdout can be undefined — fall back to error.message. */
export function gitTail(res: ReturnType<typeof spawnSync>): string {
  const stderr = (res.stderr as string | undefined) ?? "";
  const stdout = (res.stdout as string | undefined) ?? "";
  const body = stderr.trim() || stdout.trim();
  if (body) return body;
  if (res.error) return (res.error as Error).message;
  return "(no output from git)";
}
