// composerHistory provides ↑/↓ prompt-history navigation by reading a lazy
// backend "tape" of user prompts. The tape starts with the active session and
// then continues through the same session order used by the history sidebar.
//
// Unlike the old localStorage ring buffer, the session JSONL files are the
// canonical source of truth — we read them through the Go bound method
// ScanPromptHistory, which exposes cursor pages and invalidates the tape when
// sessions are created, deleted, or renamed.
//
// We don't try to do prefix-search navigation (Ctrl-R in bash) — that needs a
// search UI and a keybinding the OS doesn't already take. The arrow
// navigation is the common case and is the smallest useful addition.

import { app } from "./bridge";
import type { PromptHistoryEntry, PromptHistoryResult } from "./types";

// currentNonce identifies the backend tape. olderCursor points to the next
// unread segment; cachedEntries are only the entries the user has reached.
let currentNonce = "";
let olderCursor = "";
let hasOlder = true;
let cachedEntries: PromptHistoryEntry[] = [];
let generation = 0;
const PROMPT_HISTORY_PAGE_LIMIT = 50;

type ScanPromptHistoryResultTuple = [PromptHistoryEntry[] | null, string];
type ScanPromptHistoryResultTupleWithErr = [PromptHistoryEntry[] | null, string, unknown];
type ScanPromptHistoryResult =
  | PromptHistoryEntry[]
  | PromptHistoryResult
  | ScanPromptHistoryResultTuple
  | ScanPromptHistoryResultTupleWithErr
  | Record<string, unknown>;

interface NormalizedPromptHistoryPage {
  entries: PromptHistoryEntry[] | null;
  nonce: string;
  olderCursor: string;
  hasOlder: boolean;
}

function asEntries(value: unknown): PromptHistoryEntry[] {
  return Array.isArray(value) ? (value as PromptHistoryEntry[]) : [];
}

function maybeTupleResult(result: unknown): { entries: PromptHistoryEntry[] | null; nonce: string } | null {
  if (!Array.isArray(result)) return null;
  if (result.length >= 2 && typeof result[1] === "string") {
    return { entries: result[0] === null ? null : asEntries(result[0]), nonce: result[1] };
  }
  return null;
}

function maybeTupleMapResult(result: unknown): { entries: PromptHistoryEntry[] | null; nonce: string } | null {
  if (typeof result !== "object" || result === null) return null;
  const map = result as Record<string, unknown>;
  if (!("0" in map) || !("1" in map)) return null;
  if (typeof map["1"] !== "string") return null;
  return { entries: map["0"] === null ? null : asEntries(map["0"]), nonce: map["1"] };
}

function normalizePageResult(result: ScanPromptHistoryResult): NormalizedPromptHistoryPage {
  const tuple = maybeTupleResult(result) ?? maybeTupleMapResult(result);
  if (tuple !== null) {
    return { entries: tuple.entries, nonce: tuple.nonce, olderCursor: "", hasOlder: false };
  }

  if (Array.isArray(result)) {
    return { entries: asEntries(result), nonce: currentNonce, olderCursor: "", hasOlder: false };
  }

  if (typeof result === "object" && result !== null) {
    if ("entries" in result) {
      const rawEntries = (result as { entries?: unknown }).entries;
      const nextNonce = typeof (result as { nonce?: unknown }).nonce === "string"
        ? (result as { nonce: string }).nonce
        : currentNonce;
      const nextOlderCursor = typeof (result as { olderCursor?: unknown }).olderCursor === "string"
        ? (result as { olderCursor: string }).olderCursor
        : "";
      const nextHasOlder = typeof (result as { hasOlder?: unknown }).hasOlder === "boolean"
        ? (result as { hasOlder: boolean }).hasOlder
        : nextOlderCursor !== "";
      return {
        entries: rawEntries === null || rawEntries === undefined ? null : asEntries(rawEntries),
        nonce: nextNonce,
        olderCursor: nextOlderCursor,
        hasOlder: nextHasOlder,
      };
    }

    if (typeof (result as { nonce?: unknown }).nonce === "string") {
      return { entries: null, nonce: (result as { nonce: string }).nonce, olderCursor: "", hasOlder: false };
    }
  }

  if (result === null || result === undefined) {
    return { entries: [], nonce: currentNonce, olderCursor: "", hasOlder: false };
  }
  return { entries: asEntries(result), nonce: currentNonce, olderCursor: "", hasOlder: false };
}

// invalidateCache resets the tape so the next loadOlder() call starts from the
// current active session. Call this after any session mutation.
export function invalidateCache(): void {
  currentNonce = "";
  olderCursor = "";
  hasOlder = true;
  cachedEntries = [];
  generation++;
}

export function cacheGeneration(): number {
  return generation;
}

export async function loadOlder(): Promise<PromptHistoryEntry[]> {
  if (!hasOlder && olderCursor === "") return [];
  try {
    const request = JSON.stringify({
      nonce: currentNonce,
      cursor: olderCursor,
      limit: PROMPT_HISTORY_PAGE_LIMIT,
    });
    const result = await app.ScanPromptHistory(request);
    const page = normalizePageResult(result as ScanPromptHistoryResult);
    currentNonce = page.nonce;
    olderCursor = page.olderCursor;
    hasOlder = page.hasOlder;
    const entries = page.entries ?? [];
    if (entries.length > 0) {
      cachedEntries = cachedEntries.concat(entries);
    }
    return entries.slice();
  } catch {
    return [];
  }
}

export function hasMoreOlder(): boolean {
  return hasOlder || olderCursor !== "";
}

// snapshot returns a defensive copy of the entries loaded so far. If nothing has
// been loaded yet, it fetches the first tape page for compatibility with older
// callers and tests.
export async function snapshot(): Promise<PromptHistoryEntry[]> {
  if (cachedEntries.length === 0 && hasMoreOlder()) {
    await loadOlder();
  }
  return cachedEntries.slice();
}

// pushHistory is a no-op — prompts are persisted by the Go kernel as session
// JSONL files, so there's nothing local to append.
export function pushHistory(_text: string): void {
  // no-op: prompts are recorded by the kernel
}

// clearHistory is a no-op — session logs are the canonical store; there's
// nothing local to clear. A "clear prompt history" button would need to delete
// session files, which is a bigger operation.
export function clearHistory(): void {
  // no-op: session logs are the canonical store
}
