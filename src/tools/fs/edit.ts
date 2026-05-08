import { promises as fs } from "node:fs";
import * as pathMod from "node:path";

function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

export async function applyEdit(
  rootDir: string,
  abs: string,
  args: { search: string; replace: string },
): Promise<string> {
  if (args.search.length === 0) {
    throw new Error("edit_file: search cannot be empty");
  }
  const before = await fs.readFile(abs, "utf8");
  const le = before.includes("\r\n") ? "\r\n" : "\n";
  const adaptedSearch = args.search.replace(/\r?\n/g, le);
  const adaptedReplace = args.replace.replace(/\r?\n/g, le);
  const firstIdx = before.indexOf(adaptedSearch);
  if (firstIdx < 0) {
    throw new Error(`edit_file: search text not found in ${displayRel(rootDir, abs)}`);
  }
  const nextIdx = before.indexOf(adaptedSearch, firstIdx + 1);
  if (nextIdx >= 0) {
    throw new Error(
      `edit_file: search text appears multiple times in ${displayRel(rootDir, abs)} — include more context to disambiguate`,
    );
  }
  const after =
    before.slice(0, firstIdx) + adaptedReplace + before.slice(firstIdx + adaptedSearch.length);
  await fs.writeFile(abs, after, "utf8");
  const rel = displayRel(rootDir, abs);
  const header = `edited ${rel} (${adaptedSearch.length}→${adaptedReplace.length} chars)`;
  const startLine = before.slice(0, firstIdx).split(/\r?\n/).length;
  const diff = renderEditDiff(adaptedSearch, adaptedReplace, startLine);
  return `${header}\n${diff}`;
}

export async function applyMultiEdit(
  rootDir: string,
  abs: string,
  edits: ReadonlyArray<{ search: string; replace: string }>,
): Promise<string> {
  if (edits.length === 0) {
    throw new Error("multi_edit: edits must contain at least one entry");
  }
  const before = await fs.readFile(abs, "utf8");
  const le = before.includes("\r\n") ? "\r\n" : "\n";
  const rel = displayRel(rootDir, abs);

  let buf = before;
  const hunks: string[] = [];
  let totalDelta = 0;

  for (let i = 0; i < edits.length; i++) {
    const e = edits[i]!;
    if (e.search.length === 0) {
      throw new Error(`multi_edit: edit #${i + 1} search cannot be empty (no edits applied)`);
    }
    const adaptedSearch = e.search.replace(/\r?\n/g, le);
    const adaptedReplace = e.replace.replace(/\r?\n/g, le);
    const firstIdx = buf.indexOf(adaptedSearch);
    if (firstIdx < 0) {
      throw new Error(
        `multi_edit: edit #${i + 1} search text not found in ${rel} — no edits applied (multi_edit is atomic)`,
      );
    }
    const nextIdx = buf.indexOf(adaptedSearch, firstIdx + 1);
    if (nextIdx >= 0) {
      throw new Error(
        `multi_edit: edit #${i + 1} search text appears multiple times in ${rel} — include more context to disambiguate (no edits applied)`,
      );
    }
    const startLine = buf.slice(0, firstIdx).split(/\r?\n/).length;
    buf = buf.slice(0, firstIdx) + adaptedReplace + buf.slice(firstIdx + adaptedSearch.length);
    hunks.push(renderEditDiff(adaptedSearch, adaptedReplace, startLine));
    totalDelta += adaptedReplace.length - adaptedSearch.length;
  }

  await fs.writeFile(abs, buf, "utf8");
  const sign = totalDelta >= 0 ? "+" : "";
  const noun = edits.length === 1 ? "edit" : "edits";
  const header = `multi_edit: applied ${edits.length} ${noun} to ${rel} (${sign}${totalDelta} chars)`;
  return `${header}\n${hunks.join("\n")}`;
}

function renderEditDiff(search: string, replace: string, startLine: number): string {
  const a = search.split(/\r?\n/);
  const b = replace.split(/\r?\n/);
  const diff = lineDiff(a, b);
  const hunk = `@@ -${startLine},${a.length} +${startLine},${b.length} @@`;
  const body = diff.map((d) => `${d.op === " " ? " " : d.op} ${d.line}`).join("\n");
  return `${hunk}\n${body}`;
}

export function lineDiff(
  a: readonly string[],
  b: readonly string[],
): Array<{ op: "-" | "+" | " "; line: string }> {
  const n = a.length;
  const m = b.length;
  // dp[i][j] = LCS length of a[0..i) and b[0..j).
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = 1; i <= n; i++) {
    for (let j = 1; j <= m; j++) {
      if (a[i - 1] === b[j - 1]) dp[i]![j] = dp[i - 1]![j - 1]! + 1;
      else dp[i]![j] = Math.max(dp[i - 1]![j]!, dp[i]![j - 1]!);
    }
  }
  // Backtrack to recover the op sequence.
  const out: Array<{ op: "-" | "+" | " "; line: string }> = [];
  let i = n;
  let j = m;
  while (i > 0 && j > 0) {
    if (a[i - 1] === b[j - 1]) {
      out.unshift({ op: " ", line: a[i - 1]! });
      i--;
      j--;
    } else if ((dp[i - 1]![j] ?? 0) > (dp[i]![j - 1] ?? 0)) {
      out.unshift({ op: "-", line: a[i - 1]! });
      i--;
    } else {
      // Tie-break goes here (strictly less or equal): take the
      // insertion first during backtrack so the final forward order
      // renders removals BEFORE additions for a substitution —
      // matches git-diff convention of `- old / + new`.
      out.unshift({ op: "+", line: b[j - 1]! });
      j--;
    }
  }
  while (i > 0) {
    out.unshift({ op: "-", line: a[i - 1]! });
    i--;
  }
  while (j > 0) {
    out.unshift({ op: "+", line: b[j - 1]! });
    j--;
  }
  return out;
}
