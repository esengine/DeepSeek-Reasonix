import { formatAllBlockDiffs } from "../../code/diff-preview.js";
import type { ApplyResult, EditBlock, EditSnapshot } from "../../code/edit-blocks.js";

/** Session-only — restoring pre-apply content across restarts is git's job, not ours. */
export interface EditHistoryEntry {
  /** Sequence number within the session, stable for `/show <id>`. */
  id: number;
  /** Epoch ms when the entry was opened (first edit landed). */
  at: number;
  /** Tag for what produced the batch — "auto" / "auto-text" / "review-apply". */
  source: string;
  /** Edit blocks included in this batch, in arrival order. */
  blocks: EditBlock[];
  /** Per-block outcome — some may be "not-found" if SEARCH drifted. */
  results: ApplyResult[];
  /** First-snapshot-per-path wins — multi-edit turns roll back to pre-turn state. */
  snapshots: EditSnapshot[];
  /** Per-path so a multi-file batch can be partially undone. */
  undoneFiles: Set<string>;
}

/** True when every path in the entry has been undone. */
export function isEntryFullyUndone(e: EditHistoryEntry): boolean {
  return e.snapshots.length > 0 && e.snapshots.every((s) => e.undoneFiles.has(s.path));
}

/** Per-entry three-state status label for display. */
export function entryStatus(e: EditHistoryEntry): "applied" | "UNDONE" | "PARTIAL" {
  if (e.undoneFiles.size === 0) return "applied";
  if (isEntryFullyUndone(e)) return "UNDONE";
  return "PARTIAL";
}

/** Status prefix is `✓`/`✗` so the line reads without color (piped, screenshots). */
export function formatEditResults(results: ApplyResult[]): string {
  const lines = results.map((r) => {
    const mark = r.status === "applied" || r.status === "created" ? "✓" : "✗";
    const detail = r.message ? ` (${r.message})` : "";
    return `  ${mark} ${r.status.padEnd(11)} ${r.path}${detail}`;
  });
  const ok = results.filter((r) => r.status === "applied" || r.status === "created").length;
  const total = results.length;
  const header = `▸ edit blocks: ${ok}/${total} applied — /undo to roll back, or \`git diff\` to review`;
  return [header, ...lines].join("\n");
}

/** `[N]` labels so users can `/apply 1,3-4` instead of all-or-nothing. */
export function formatPendingPreview(blocks: EditBlock[]): string {
  const partial = blocks.length > 1 ? "  ·  /apply N or 1,3-4 for partial" : "";
  const header = `▸ ${blocks.length} pending edit block(s) — /apply (or y) to commit · /discard (or n) to drop${partial}`;
  const diffLines = formatAllBlockDiffs(blocks, { numbered: blocks.length > 1 });
  return [header, ...diffLines].join("\n");
}

/** Empty input → `{ ok: [] }` so callers detect "no indices" → default to all-blocks. */
export function parseEditIndices(raw: string, max: number): { ok: number[] } | { error: string } {
  const trimmed = raw.trim();
  if (!trimmed) return { ok: [] };
  if (max <= 0) return { error: "no pending edits to address" };
  const seen = new Set<number>();
  const tokens = trimmed
    .split(",")
    .map((t) => t.trim())
    .filter((t) => t.length > 0);
  if (tokens.length === 0) return { ok: [] };
  for (const tok of tokens) {
    const range = tok.match(/^(\d+)-(\d+)$/);
    if (range) {
      const a = Number.parseInt(range[1] ?? "", 10);
      const b = Number.parseInt(range[2] ?? "", 10);
      if (!Number.isFinite(a) || !Number.isFinite(b) || a < 1 || b < 1) {
        return { error: `invalid range: "${tok}"` };
      }
      const lo = Math.min(a, b);
      const hi = Math.max(a, b);
      if (hi > max) return { error: `index ${hi} out of range (max ${max})` };
      for (let i = lo; i <= hi; i++) seen.add(i);
      continue;
    }
    if (!/^\d+$/.test(tok)) return { error: `invalid index: "${tok}"` };
    const n = Number.parseInt(tok, 10);
    if (!Number.isFinite(n) || n < 1) return { error: `invalid index: "${tok}"` };
    if (n > max) return { error: `index ${n} out of range (max ${max})` };
    seen.add(n);
  }
  return { ok: [...seen].sort((a, b) => a - b) };
}

export function partitionEdits<T>(
  edits: readonly T[],
  indices1Based: readonly number[],
): { selected: T[]; remaining: T[] } {
  const picked = new Set(indices1Based);
  const selected: T[] = [];
  const remaining: T[] = [];
  for (let i = 0; i < edits.length; i++) {
    if (picked.has(i + 1)) selected.push(edits[i] as T);
    else remaining.push(edits[i] as T);
  }
  return { selected, remaining };
}

export function formatUndoRows(results: ApplyResult[]): string[] {
  return results.map((r) => {
    const mark = r.status === "applied" ? "✓" : "✗";
    const detail = r.message ? ` (${r.message})` : "";
    return `  ${mark} ${r.path}${detail}`;
  });
}

export function describeRepair(repair: {
  scavenged: number;
  truncationsFixed: number;
  stormsBroken: number;
}): string {
  const parts: string[] = [];
  if (repair.scavenged) parts.push(`scavenged ${repair.scavenged}`);
  if (repair.truncationsFixed) parts.push(`repaired ${repair.truncationsFixed} truncation`);
  if (repair.stormsBroken) parts.push(`broke ${repair.stormsBroken} storm`);
  return parts.length ? `[repair] ${parts.join(", ")}` : "";
}
