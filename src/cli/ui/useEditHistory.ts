import { useCallback, useRef, useState } from "react";
import { formatAllBlockDiffs } from "../../code/diff-preview.js";
import {
  type ApplyResult,
  type EditBlock,
  type EditSnapshot,
  restoreSnapshots,
} from "../../code/edit-blocks.js";
import {
  type EditHistoryEntry,
  entryStatus,
  formatUndoRows,
  isEntryFullyUndone,
} from "./edit-history.js";

export interface UndoBannerState {
  results: ApplyResult[];
  expiresAt: number;
  /** Set when the user paused the countdown; banner stays up until they resume or hit `u`. */
  pausedRemainingMs: number | null;
}

export interface UseEditHistoryResult {
  /** Post-auto-apply banner state — rendered at the bottom for 5s. */
  undoBanner: UndoBannerState | null;
  /** First-wins-per-path within an open turn — `/undo` restores pre-turn state, not a half-edit. */
  recordEdit: (
    source: string,
    blocks: readonly EditBlock[],
    results: readonly ApplyResult[],
    snaps: readonly EditSnapshot[],
  ) => void;
  /** Replaces the dismiss timer so multiple edits in one turn don't prematurely expire the window. */
  armUndoBanner: (results: ApplyResult[]) => void;
  /** Pause / resume the active undo countdown. No-ops if the banner is already settled. */
  toggleUndoPause: () => void;
  codeUndo: (args?: readonly string[]) => string;
  codeHistory: () => string;
  codeShowEdit: (args?: readonly string[]) => string;
  /** Sealed at handleSubmit start so prior turns stay intact for independent /history walks. */
  sealCurrentEntry: () => void;
  /** Reads the ref fresh — callers must re-read each time. */
  hasUndoable: () => boolean;
  /** Includes paths from undone batches — they're still files the user was thinking about. */
  touchedPaths: () => string[];
}

/** `codeMode` undefined → all handlers no-op (hook is always mounted). */
export function useEditHistory(codeMode: { rootDir: string } | undefined): UseEditHistoryResult {
  const editHistory = useRef<EditHistoryEntry[]>([]);
  const nextHistoryId = useRef(1);
  const currentTurnEntry = useRef<EditHistoryEntry | null>(null);
  const [undoBanner, setUndoBanner] = useState<UndoBannerState | null>(null);
  const undoTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const recordEdit = useCallback<UseEditHistoryResult["recordEdit"]>(
    (source, blocks, results, snaps) => {
      if (snaps.length === 0) return;
      let entry = currentTurnEntry.current;
      if (!entry) {
        entry = {
          id: nextHistoryId.current++,
          at: Date.now(),
          source,
          blocks: [],
          results: [],
          snapshots: [],
          undoneFiles: new Set<string>(),
        };
        currentTurnEntry.current = entry;
        editHistory.current.push(entry);
      }
      entry.blocks.push(...blocks);
      entry.results.push(...results);
      const seen = new Set(entry.snapshots.map((s) => s.path));
      for (const s of snaps) {
        if (!seen.has(s.path)) entry.snapshots.push(s);
      }
    },
    [],
  );

  const armUndoBanner = useCallback<UseEditHistoryResult["armUndoBanner"]>((results) => {
    setUndoBanner({ results, expiresAt: Date.now() + 5000, pausedRemainingMs: null });
    if (undoTimeoutRef.current) clearTimeout(undoTimeoutRef.current);
    undoTimeoutRef.current = setTimeout(() => {
      setUndoBanner(null);
      undoTimeoutRef.current = null;
    }, 5000);
  }, []);

  const toggleUndoPause = useCallback<UseEditHistoryResult["toggleUndoPause"]>(() => {
    setUndoBanner((prev) => {
      if (!prev) return prev;
      if (prev.pausedRemainingMs === null) {
        const remaining = Math.max(0, prev.expiresAt - Date.now());
        if (undoTimeoutRef.current) {
          clearTimeout(undoTimeoutRef.current);
          undoTimeoutRef.current = null;
        }
        return { ...prev, pausedRemainingMs: remaining };
      }
      const remaining = prev.pausedRemainingMs;
      undoTimeoutRef.current = setTimeout(() => {
        setUndoBanner(null);
        undoTimeoutRef.current = null;
      }, remaining);
      return { ...prev, expiresAt: Date.now() + remaining, pausedRemainingMs: null };
    });
  }, []);

  const codeUndo = useCallback<UseEditHistoryResult["codeUndo"]>(
    (args = []) => {
      if (!codeMode) return "not in code mode";
      const root = codeMode.rootDir;

      const revert = (entry: EditHistoryEntry, paths: readonly string[]): string => {
        const subset = entry.snapshots.filter((s) => paths.includes(s.path));
        if (subset.length === 0) {
          return `batch #${entry.id}: nothing to undo (already restored or path not in batch)`;
        }
        const results = restoreSnapshots(subset, root);
        for (const s of subset) entry.undoneFiles.add(s.path);
        if (currentTurnEntry.current === entry && isEntryFullyUndone(entry)) {
          currentTurnEntry.current = null;
        }
        if (undoTimeoutRef.current) {
          clearTimeout(undoTimeoutRef.current);
          undoTimeoutRef.current = null;
        }
        setUndoBanner(null);
        const when = new Date(entry.at).toISOString().replace("T", " ").slice(11, 19);
        const scope = subset.length === 1 ? subset[0]!.path : `${subset.length} file(s)`;
        const header = `▸ undo: reverted ${scope} from batch #${entry.id} (${when})`;
        return [header, ...formatUndoRows(results)].join("\n");
      };

      const idArg = args[0];
      const pathArg = args[1];

      if (!idArg) {
        for (let i = editHistory.current.length - 1; i >= 0; i--) {
          const e = editHistory.current[i]!;
          if (isEntryFullyUndone(e)) continue;
          const remaining = e.snapshots.map((s) => s.path).filter((p) => !e.undoneFiles.has(p));
          return revert(e, remaining);
        }
        return "nothing to undo — every batch in the session history is already undone";
      }

      const id = Number.parseInt(idArg, 10);
      if (!Number.isFinite(id)) {
        return "usage: /undo [id] [path]   (omit id for newest; id from /history; path from /show <id>)";
      }
      const entry = editHistory.current.find((e) => e.id === id);
      if (!entry) return `no edit #${id} — run /history to see valid ids`;

      if (!pathArg) {
        const remaining = entry.snapshots
          .map((s) => s.path)
          .filter((p) => !entry.undoneFiles.has(p));
        if (remaining.length === 0) return `batch #${id} is already fully undone`;
        return revert(entry, remaining);
      }

      const snap = entry.snapshots.find((s) => s.path === pathArg);
      if (!snap) {
        const files = [...new Set(entry.blocks.map((b) => b.path))];
        return `batch #${id} doesn't include "${pathArg}" — files in this batch: ${files.join(", ")}`;
      }
      if (entry.undoneFiles.has(pathArg)) {
        return `${pathArg} in batch #${id} is already undone`;
      }
      return revert(entry, [pathArg]);
    },
    [codeMode],
  );

  const codeHistory = useCallback<UseEditHistoryResult["codeHistory"]>(() => {
    if (!codeMode) return "not in code mode";
    const entries = editHistory.current;
    if (entries.length === 0) return "no edits recorded this session yet";
    const lines = ["Edit history (oldest first):"];
    for (const e of entries) {
      const when = new Date(e.at).toISOString().replace("T", " ").slice(11, 19);
      const files = new Set(e.blocks.map((b) => b.path));
      const fileList = [...files].join(", ");
      const fileSummary = fileList.length > 60 ? `${fileList.slice(0, 60)}…` : fileList;
      const status = entryStatus(e);
      const statusText =
        status === "applied" ? "applied" : status === "PARTIAL" ? "PARTIAL" : "UNDONE ";
      lines.push(
        `  #${String(e.id).padStart(3)}  ${when}  ${statusText}  ${e.source.padEnd(12)} ${files.size} file · ${e.blocks.length} block   ${fileSummary}`,
      );
    }
    lines.push("");
    lines.push(
      "/show <id>            → per-file summary    ·    /show <id> <path>  → full diff of one file",
    );
    lines.push(
      "/undo                 → newest non-undone   ·    /undo <id> [path]  → target a specific batch or file",
    );
    return lines.join("\n");
  }, [codeMode]);

  const codeShowEdit = useCallback<UseEditHistoryResult["codeShowEdit"]>(
    (args = []) => {
      if (!codeMode) return "not in code mode";
      const entries = editHistory.current;
      if (entries.length === 0) return "no edits recorded this session — /history is empty";

      const idArg = args[0];
      const pathArg = args[1];

      let entry: EditHistoryEntry | undefined;
      if (!idArg) {
        entry =
          [...entries].reverse().find((e) => !isEntryFullyUndone(e)) ?? entries[entries.length - 1];
      } else {
        const id = Number.parseInt(idArg, 10);
        if (!Number.isFinite(id)) {
          return "usage: /show [id] [path]   (omit id for newest; path from the per-file summary)";
        }
        entry = entries.find((e) => e.id === id);
        if (!entry) return `no edit #${id} — run /history to see valid ids`;
      }
      if (!entry) return "unexpected: history lookup failed";

      if (pathArg) {
        const fileBlocks = entry.blocks.filter((b) => b.path === pathArg);
        if (fileBlocks.length === 0) {
          const files = [...new Set(entry.blocks.map((b) => b.path))];
          return `batch #${entry.id} doesn't include "${pathArg}" — files in this batch: ${files.join(", ")}`;
        }
        const when = new Date(entry.at).toISOString().replace("T", " ").slice(11, 19);
        const state = entry.undoneFiles.has(pathArg) ? "UNDONE" : "applied";
        const header = `▸ edit #${entry.id} · ${when} · ${pathArg} · ${state} · ${fileBlocks.length} block(s)`;
        const diff = formatAllBlockDiffs(fileBlocks, { maxLines: 60, contextLines: 2 });
        const footer = entry.undoneFiles.has(pathArg)
          ? "(already reverted — /history shows the batch-level status)"
          : `/undo ${entry.id} ${pathArg}  → revert just this file`;
        return [header, ...diff, "", footer].join("\n");
      }

      const when = new Date(entry.at).toISOString().replace("T", " ").slice(11, 19);
      const files = [...new Set(entry.blocks.map((b) => b.path))];
      const status = entryStatus(entry);
      const header = `▸ edit #${entry.id} · ${when} · ${entry.source} · ${status} · ${files.length} file(s)`;
      const countLines = (s: string) => (s.length === 0 ? 0 : (s.match(/\n/g)?.length ?? 0) + 1);
      const fileLines = files.map((path) => {
        const fileBlocks = entry!.blocks.filter((b) => b.path === path);
        let removed = 0;
        let added = 0;
        for (const b of fileBlocks) {
          removed += countLines(b.search);
          added += countLines(b.replace);
        }
        const state = entry!.undoneFiles.has(path) ? "UNDONE" : "applied";
        return `  ${state.padEnd(7)}  -${String(removed).padStart(3)}/+${String(added).padStart(3)}   ${path}  (${fileBlocks.length} block${fileBlocks.length === 1 ? "" : "s"})`;
      });
      return [
        header,
        ...fileLines,
        "",
        `/show ${entry.id} <path>   → full diff of one file`,
        `/undo ${entry.id} <path>   → revert just that file   ·   /undo ${entry.id} → revert whole batch`,
      ].join("\n");
    },
    [codeMode],
  );

  const sealCurrentEntry = useCallback(() => {
    currentTurnEntry.current = null;
  }, []);

  const hasUndoable = useCallback(
    () => editHistory.current.some((e) => !isEntryFullyUndone(e)),
    [],
  );

  const touchedPaths = useCallback<UseEditHistoryResult["touchedPaths"]>(() => {
    const seen = new Set<string>();
    for (const entry of editHistory.current) {
      for (const b of entry.blocks) seen.add(b.path);
    }
    return [...seen];
  }, []);

  return {
    undoBanner,
    recordEdit,
    armUndoBanner,
    toggleUndoPause,
    codeUndo,
    codeHistory,
    codeShowEdit,
    sealCurrentEntry,
    hasUndoable,
    touchedPaths,
  };
}
