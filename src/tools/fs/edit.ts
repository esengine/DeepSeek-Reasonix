import { promises as fs } from "node:fs";
import * as pathMod from "node:path";
import { type FileEncoding, decodeFileBuffer, encodeFile } from "../../code/file-encoding.js";

function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

/** Marker substring in the gate-reject message so tools.ts's repeat-rejection tracker spots a 2nd identical unread-edit and switches to the sharper "stop retrying" hint. */
export const READ_BEFORE_EDIT_MARKER = "read_file first";

export async function applyEdit(
  rootDir: string,
  abs: string,
  args: { search: string; replace: string },
  hasRead?: (abs: string) => boolean,
): Promise<string> {
  if (args.search.length === 0) {
    throw new Error("edit_file: search cannot be empty");
  }
  if (hasRead && !hasRead(abs)) {
    throw new Error(
      `edit_file: ${displayRel(rootDir, abs)} was not read this session — ${READ_BEFORE_EDIT_MARKER} so your SEARCH matches the bytes on disk.`,
    );
  }
  const beforeBuf = await fs.readFile(abs);
  const { text: before, encoding } = decodeFileBuffer(beforeBuf);
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
  await fs.writeFile(abs, encodeFile(after, encoding));
  const rel = displayRel(rootDir, abs);
  const header = `edited ${rel} (${adaptedSearch.length}→${adaptedReplace.length} chars)`;
  const startLine = before.slice(0, firstIdx).split(/\r?\n/).length;
  const diff = renderEditDiff(adaptedSearch, adaptedReplace, startLine);
  return `${header}\n${diff}`;
}

export interface DeleteRangeArgs {
  start_anchor: string;
  end_anchor: string;
  inclusive?: boolean;
}

export interface DeletePatch {
  search: string;
  replace: "";
  startIndex: number;
  endIndex: number;
  startLine: number;
  deletedChars: number;
  noopReason?: string;
}

export function computeDeleteRangePatchFromText(text: string, args: DeleteRangeArgs): DeletePatch {
  const startAnchor = args.start_anchor;
  const endAnchor = args.end_anchor;
  if (typeof startAnchor !== "string" || startAnchor.length === 0) {
    return noDeletePatch("start_anchor is empty");
  }
  if (typeof endAnchor !== "string" || endAnchor.length === 0) {
    return noDeletePatch("end_anchor is empty");
  }
  const startHits = allOccurrences(text, startAnchor);
  if (startHits.length !== 1) {
    return noDeletePatch(
      startHits.length === 0
        ? "start_anchor not found"
        : `start_anchor appears ${startHits.length} times`,
    );
  }
  const endHits = allOccurrences(text, endAnchor);
  if (endHits.length !== 1) {
    return noDeletePatch(
      endHits.length === 0 ? "end_anchor not found" : `end_anchor appears ${endHits.length} times`,
    );
  }
  const inclusive = args.inclusive !== false;
  const startIdx = startHits[0]!;
  const endIdx = endHits[0]!;
  const deleteStart = inclusive ? startIdx : startIdx + startAnchor.length;
  const deleteEnd = inclusive ? endIdx + endAnchor.length : endIdx;
  if (deleteStart > deleteEnd) {
    return noDeletePatch("start_anchor resolves after end_anchor");
  }
  if (deleteStart === deleteEnd) {
    return noDeletePatch("anchor range is empty");
  }
  const search = text.slice(deleteStart, deleteEnd);
  return {
    search,
    replace: "",
    startIndex: deleteStart,
    endIndex: deleteEnd,
    startLine: text.slice(0, deleteStart).split(/\r?\n/).length,
    deletedChars: search.length,
  };
}

export function computeDeleteLineRangePatchFromText(
  text: string,
  startLine: number,
  endLine: number,
): DeletePatch {
  if (!Number.isInteger(startLine) || !Number.isInteger(endLine)) {
    return noDeletePatch("line range must be integer lines");
  }
  if (startLine < 1 || endLine < startLine) {
    return noDeletePatch("line range is invalid");
  }
  const starts = lineStartOffsets(text);
  if (startLine > starts.length) {
    return noDeletePatch(`start line ${startLine} is outside the file`);
  }
  const startIdx = starts[startLine - 1]!;
  const endIdx = starts[endLine] ?? text.length;
  if (startIdx >= endIdx) return noDeletePatch("line range is empty");
  const search = text.slice(startIdx, endIdx);
  return {
    search,
    replace: "",
    startIndex: startIdx,
    endIndex: endIdx,
    startLine,
    deletedChars: search.length,
  };
}

export async function applyDeleteRange(
  rootDir: string,
  abs: string,
  args: DeleteRangeArgs,
  hasRead?: (abs: string) => boolean,
): Promise<string> {
  if (hasRead && !hasRead(abs)) {
    throw new Error(
      `delete_range: ${displayRel(rootDir, abs)} was not read this session — ${READ_BEFORE_EDIT_MARKER} so anchors match the bytes on disk.`,
    );
  }
  const beforeBuf = await fs.readFile(abs);
  const { text: before, encoding } = decodeFileBuffer(beforeBuf);
  const le = before.includes("\r\n") ? "\r\n" : "\n";
  const patch = computeDeleteRangePatchFromText(before, {
    ...args,
    start_anchor: args.start_anchor.replace(/\r?\n/g, le),
    end_anchor: args.end_anchor.replace(/\r?\n/g, le),
  });
  const rel = displayRel(rootDir, abs);
  if (patch.noopReason) return `delete_range: no-op for ${rel} — ${patch.noopReason}`;
  const after = `${before.slice(0, patch.startIndex)}${patch.replace}${before.slice(patch.endIndex)}`;
  await fs.writeFile(abs, encodeFile(after, encoding));
  return `delete_range: deleted ${patch.deletedChars} chars from ${rel}\n${renderEditDiff(patch.search, patch.replace, patch.startLine)}`;
}

export async function applyDeleteLineRange(
  rootDir: string,
  abs: string,
  startLine: number,
  endLine: number,
  toolName: string,
  hasRead?: (abs: string) => boolean,
): Promise<string> {
  if (hasRead && !hasRead(abs)) {
    throw new Error(
      `${toolName}: ${displayRel(rootDir, abs)} was not read this session — ${READ_BEFORE_EDIT_MARKER} so deletion matches the bytes on disk.`,
    );
  }
  const beforeBuf = await fs.readFile(abs);
  const { text: before, encoding } = decodeFileBuffer(beforeBuf);
  const patch = computeDeleteLineRangePatchFromText(before, startLine, endLine);
  const rel = displayRel(rootDir, abs);
  if (patch.noopReason) return `${toolName}: no-op for ${rel} — ${patch.noopReason}`;
  const after = `${before.slice(0, patch.startIndex)}${patch.replace}${before.slice(patch.endIndex)}`;
  await fs.writeFile(abs, encodeFile(after, encoding));
  return `${toolName}: deleted lines ${startLine}-${endLine} from ${rel}\n${renderEditDiff(patch.search, patch.replace, patch.startLine)}`;
}

export function expandSymbolDeletionStartLine(
  lines: readonly string[],
  symbolLine: number,
): number {
  let startLine = symbolLine;

  startLine = expandDecoratorStartLine(lines, startLine);
  startLine = expandDocCommentStartLine(lines, startLine);
  return expandDecoratorStartLine(lines, startLine);
}

function noDeletePatch(reason: string): DeletePatch {
  return {
    search: "",
    replace: "",
    startIndex: 0,
    endIndex: 0,
    startLine: 1,
    deletedChars: 0,
    noopReason: reason,
  };
}

function allOccurrences(haystack: string, needle: string): number[] {
  const out: number[] = [];
  let idx = haystack.indexOf(needle);
  while (idx >= 0) {
    out.push(idx);
    idx = haystack.indexOf(needle, idx + Math.max(1, needle.length));
  }
  return out;
}

function lineStartOffsets(text: string): number[] {
  const starts = [0];
  for (let i = 0; i < text.length; i++) {
    if (text[i] === "\n" && i + 1 < text.length) starts.push(i + 1);
  }
  return starts;
}

function expandDecoratorStartLine(lines: readonly string[], startLine: number): number {
  let nextStart = startLine;
  while (nextStart > 1) {
    const previous = (lines[nextStart - 2] ?? "").trim();
    if (previous.startsWith("@")) {
      nextStart--;
      continue;
    }
    if (looksLikeDecoratorContinuation(previous)) {
      const decoratorStart = findDecoratorStartAbove(lines, nextStart - 1);
      if (decoratorStart !== null) {
        nextStart = decoratorStart;
        continue;
      }
    }
    break;
  }
  return nextStart;
}

function looksLikeDecoratorContinuation(trimmed: string): boolean {
  return /^[)\]}]/.test(trimmed);
}

function findDecoratorStartAbove(lines: readonly string[], lastLine: number): number | null {
  for (let line = lastLine; line >= 1; line--) {
    const trimmed = (lines[line - 1] ?? "").trim();
    if (trimmed.length === 0) return null;
    if (trimmed.startsWith("@")) return line;
  }
  return null;
}

function expandDocCommentStartLine(lines: readonly string[], startLine: number): number {
  if (startLine <= 1) return startLine;
  const previous = (lines[startLine - 2] ?? "").trim();
  if (!previous.endsWith("*/")) return startLine;
  for (let line = startLine - 1; line >= 1; line--) {
    const trimmed = (lines[line - 1] ?? "").trim();
    if (trimmed.startsWith("/**")) return line;
    if (!trimmed.startsWith("*") && trimmed.length > 0) return startLine;
  }
  return startLine;
}

export interface MultiEditEntry {
  abs: string;
  search: string;
  replace: string;
}

export async function applyMultiEdit(
  rootDir: string,
  edits: ReadonlyArray<MultiEditEntry>,
  hasRead?: (abs: string) => boolean,
): Promise<string> {
  if (edits.length === 0) {
    throw new Error("multi_edit: edits must contain at least one entry");
  }
  type FileState = {
    before: string;
    buf: string;
    le: string;
    hunks: string[];
    deltaChars: number;
    touched: number;
    encoding: FileEncoding;
  };
  const filesByPath = new Map<string, FileState>();

  for (let i = 0; i < edits.length; i++) {
    const e = edits[i]!;
    if (typeof e.abs !== "string" || e.abs.length === 0) {
      throw new Error(`multi_edit: edit #${i + 1} requires a string \`path\` (no edits applied)`);
    }
    if (typeof e.search !== "string") {
      throw new Error(`multi_edit: edit #${i + 1} requires a string \`search\` (no edits applied)`);
    }
    if (typeof e.replace !== "string") {
      throw new Error(
        `multi_edit: edit #${i + 1} requires a string \`replace\` (no edits applied)`,
      );
    }
    const rel = displayRel(rootDir, e.abs);
    if (e.search.length === 0) {
      throw new Error(
        `multi_edit: edit #${i + 1} (${rel}) search cannot be empty (no edits applied)`,
      );
    }
    let state = filesByPath.get(e.abs);
    if (!state) {
      if (hasRead && !hasRead(e.abs)) {
        throw new Error(
          `multi_edit: edit #${i + 1} target ${rel} was not read this session — ${READ_BEFORE_EDIT_MARKER} (no edits applied)`,
        );
      }
      let before: string;
      let encoding: FileEncoding;
      try {
        const buf = await fs.readFile(e.abs);
        ({ text: before, encoding } = decodeFileBuffer(buf));
      } catch (err) {
        throw new Error(
          `multi_edit: edit #${i + 1} cannot read ${rel}: ${(err as Error).message} (no edits applied)`,
        );
      }
      const le = before.includes("\r\n") ? "\r\n" : "\n";
      state = { before, buf: before, le, hunks: [], deltaChars: 0, touched: 0, encoding };
      filesByPath.set(e.abs, state);
    }
    const adaptedSearch = e.search.replace(/\r?\n/g, state.le);
    const adaptedReplace = e.replace.replace(/\r?\n/g, state.le);
    const firstIdx = state.buf.indexOf(adaptedSearch);
    if (firstIdx < 0) {
      throw new Error(
        `multi_edit: edit #${i + 1} search text not found in ${rel} — no edits applied`,
      );
    }
    const nextIdx = state.buf.indexOf(adaptedSearch, firstIdx + 1);
    if (nextIdx >= 0) {
      throw new Error(
        `multi_edit: edit #${i + 1} search text appears multiple times in ${rel} — include more context to disambiguate (no edits applied)`,
      );
    }
    const startLine = state.buf.slice(0, firstIdx).split(/\r?\n/).length;
    state.buf =
      state.buf.slice(0, firstIdx) +
      adaptedReplace +
      state.buf.slice(firstIdx + adaptedSearch.length);
    state.hunks.push(`# ${rel}\n${renderEditDiff(adaptedSearch, adaptedReplace, startLine)}`);
    state.deltaChars += adaptedReplace.length - adaptedSearch.length;
    state.touched++;
  }

  // Push to `attempted` BEFORE writeFile so a write that truncates or
  // partially-writes before failing is also rolled back.
  const attempted: Array<{ abs: string; before: string; encoding: FileEncoding }> = [];
  try {
    for (const [abs, state] of filesByPath) {
      attempted.push({ abs, before: state.before, encoding: state.encoding });
      await fs.writeFile(abs, encodeFile(state.buf, state.encoding));
    }
  } catch (writeErr) {
    const rollbackFailures: string[] = [];
    for (const item of [...attempted].reverse()) {
      try {
        await fs.writeFile(item.abs, encodeFile(item.before, item.encoding));
      } catch (restoreErr) {
        rollbackFailures.push(`${displayRel(rootDir, item.abs)}: ${(restoreErr as Error).message}`);
      }
    }
    if (rollbackFailures.length > 0) {
      throw new Error(
        `multi_edit: write failed after partial application: ${(writeErr as Error).message}; rollback failed for ${rollbackFailures.join("; ")}`,
      );
    }
    throw new Error(
      `multi_edit: write failed: ${(writeErr as Error).message}; rolled back all files that may have been modified`,
    );
  }

  const fileCount = filesByPath.size;
  const editCount = edits.length;
  let totalDelta = 0;
  const allHunks: string[] = [];
  for (const state of filesByPath.values()) {
    totalDelta += state.deltaChars;
    allHunks.push(...state.hunks);
  }
  const sign = totalDelta >= 0 ? "+" : "";
  const editNoun = editCount === 1 ? "edit" : "edits";
  const fileNoun = fileCount === 1 ? "file" : "files";
  const header = `multi_edit: applied ${editCount} ${editNoun} across ${fileCount} ${fileNoun} (${sign}${totalDelta} chars)`;
  return `${header}\n${allHunks.join("\n")}`;
}

function renderEditDiff(search: string, replace: string, startLine: number): string {
  const a = search.split(/\r?\n/);
  const b = replace.split(/\r?\n/);
  const diff = lineDiff(a, b);
  const hunk = `@@ -${startLine},${a.length} +${startLine},${b.length} @@`;
  const body = diff.map((d) => `${d.op === " " ? " " : d.op} ${d.line}`).join("\n");
  return `${hunk}\n${body}`;
}

/** Thresholds beyond which write_file skips full diff to avoid slow LCS on huge files. */
const WRITE_DIFF_MAX_LINES = 5000;
const WRITE_DIFF_MAX_BYTES = 100 * 1024;

/** Generate write_file result with unified diff, matching edit_file format. New file → `created path (N chars)`, overwrite → `edited path (old→new chars)` + diff, large file → summary only. */
export function generateWriteDiff(
  oldContent: string | null,
  newContent: string,
  rel: string,
): string {
  const newLen = newContent.length;

  // New file — no old content to diff against.
  if (oldContent === null) {
    return `created ${rel} (${newLen} chars)`;
  }

  const oldLen = oldContent.length;

  // No changes.
  if (oldContent === newContent) {
    return `edited ${rel} (${oldLen}→${newLen} chars)`;
  }

  // Large file — skip diff computation to avoid O(n*m) LCS blowup.
  if (
    oldContent.length > WRITE_DIFF_MAX_BYTES ||
    newContent.length > WRITE_DIFF_MAX_BYTES ||
    oldContent.split(/\r?\n/).length > WRITE_DIFF_MAX_LINES ||
    newContent.split(/\r?\n/).length > WRITE_DIFF_MAX_LINES
  ) {
    return `edited ${rel} (${oldLen}→${newLen} chars) [diff too large]`;
  }

  const header = `edited ${rel} (${oldLen}→${newLen} chars)`;
  const oldLines = oldContent.split(/\r?\n/);
  const newLines = newContent.split(/\r?\n/);
  const diff = lineDiff(oldLines, newLines);
  const hunk = `@@ -1,${oldLines.length} +1,${newLines.length} @@`;
  const body = diff.map((d) => `${d.op === " " ? " " : d.op} ${d.line}`).join("\n");
  return `${header}\n${hunk}\n${body}`;
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
