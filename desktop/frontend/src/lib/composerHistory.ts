// composerHistory is a tiny ring buffer of the user's last sent prompts with
// ⌥↑/⌥↓ navigation. The history is shared across all sessions (the same way
// a shell history is — having "fix the lint errors" only available in the
// session it was first sent in would defeat the point), capped at 200 entries
// to bound localStorage, and deduped to keep the most recent occurrence of any
// repeated prompt at the top (mirroring the .bash_history "HISTCONTROL=
// erasedups" convention).
//
// We don't try to do prefix-search navigation (Ctrl-R in bash) — that needs a
// search UI and a keybinding the OS doesn't already take. The arrow
// navigation is the common case and is the smallest useful addition.

const KEY = "reasonix.composer.history";
const CAP = 200;

export interface HistoryEntry {
  // text is the exact string the user sent (post-trim, attachments joined
  // as @path tokens). We store the text verbatim so the user can edit a
  // recalled prompt with a single keystroke rather than starting from a
  // "rewritten" version.
  text: string;
  // at is the wall-clock ms when the prompt was sent. Used to display a
  // tooltip like "Sent 3h ago" on the optional history preview pill (a
  // future addition; right now the field is here so adding the pill is a
  // one-line change).
  at: number;
}

function readAll(): HistoryEntry[] {
  if (typeof localStorage === "undefined") return [];
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const v = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return v.filter((e): e is HistoryEntry => !!e && typeof e.text === "string" && typeof e.at === "number");
  } catch {
    return [];
  }
}

function writeAll(list: HistoryEntry[]): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    /* private mode — fine to forget */
  }
}

// push adds `text` to the history. Same-text entries are deduped (the new
// entry takes the new timestamp) and the list is capped at CAP. We don't
// dedupe across whitespace differences — "fix lint" and "fix  lint" are
// different prompts the user typed, and silently coalescing them would
// confuse recall.
export function pushHistory(text: string): void {
  const trimmed = text.trim();
  if (!trimmed) return;
  const all = readAll();
  const without = all.filter((e) => e.text !== trimmed);
  without.unshift({ text: trimmed, at: Date.now() });
  if (without.length > CAP) without.length = CAP;
  writeAll(without);
}

// snapshot returns the current history, most-recent first. The list is
// returned as a defensive copy so callers can't mutate the cache by
// accident.
export function snapshot(): HistoryEntry[] {
  return readAll().slice();
}

// clear wipes the persisted history. The settings panel may eventually
// expose a "clear prompt history" button — the function is here so that
// addition is a 2-line change.
export function clearHistory(): void {
  writeAll([]);
}
