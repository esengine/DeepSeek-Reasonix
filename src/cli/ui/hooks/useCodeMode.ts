import { type MutableRefObject, useCallback } from "react";
import {
  type ApplyResult,
  type EditBlock,
  type EditSnapshot,
  applyEditBlocks,
  snapshotBeforeEdits,
} from "../../../code/edit-blocks.js";
import { clearPendingEdits, savePendingEdits } from "../../../code/pending-edits.js";
import { t } from "../../../i18n/index.js";
import { formatEditResults, partitionEdits } from "../edit-history.js";

export interface UseCodeModeResult {
  /** /apply callback. Empty `indices` means "all"; specific 1-based indices apply only those. */
  codeApply: (indices?: readonly number[]) => string;
  /** /discard callback. Same indices semantics as codeApply. */
  codeDiscard: (indices?: readonly number[]) => string;
}

export interface UseCodeModeOptions {
  codeMode: boolean;
  pendingEdits: MutableRefObject<EditBlock[]>;
  currentRootDir: string;
  session: string | null;
  syncPendingCount: () => void;
  recordEdit: (
    source: string,
    blocks: readonly EditBlock[],
    results: readonly ApplyResult[],
    snaps: readonly EditSnapshot[],
  ) => void;
}

/** Slash-command callbacks for `/apply` and `/discard` over the pending-edits queue. Owns the partition / snapshot / save / sync sequence; AppInner just forwards the strings to its log. */
export function useCodeMode(opts: UseCodeModeOptions): UseCodeModeResult {
  const { codeMode, pendingEdits, currentRootDir, session, syncPendingCount, recordEdit } = opts;

  const codeApply = useCallback(
    (indices?: readonly number[]): string => {
      if (!codeMode) return t("app.editHistoryNoCodeMode");
      const blocks = pendingEdits.current;
      if (blocks.length === 0) {
        return t("app.noPendingEdits");
      }
      const useSubset = indices !== undefined && indices.length > 0;
      const { selected, remaining } = useSubset
        ? partitionEdits(blocks, indices)
        : { selected: blocks, remaining: [] as EditBlock[] };
      if (selected.length === 0) {
        return t("app.noMatchedApply");
      }
      const snaps = snapshotBeforeEdits(selected, currentRootDir);
      const results = applyEditBlocks(selected, currentRootDir);
      const anyApplied = results.some((r) => r.status === "applied" || r.status === "created");
      if (anyApplied) recordEdit("review-apply", selected, results, snaps);
      pendingEdits.current = remaining;
      if (remaining.length === 0) clearPendingEdits(session ?? null);
      else savePendingEdits(session ?? null, remaining);
      syncPendingCount();
      const tail =
        remaining.length > 0 ? `\n${t("app.blocksStillPending", { count: remaining.length })}` : "";
      return formatEditResults(results) + tail;
    },
    [codeMode, currentRootDir, session, syncPendingCount, recordEdit, pendingEdits],
  );

  const codeDiscard = useCallback(
    (indices?: readonly number[]): string => {
      const blocks = pendingEdits.current;
      if (blocks.length === 0) return t("app.noPendingDiscard");
      const useSubset = indices !== undefined && indices.length > 0;
      const { selected, remaining } = useSubset
        ? partitionEdits(blocks, indices)
        : { selected: blocks, remaining: [] as EditBlock[] };
      if (selected.length === 0) {
        return t("app.noMatchedDiscard");
      }
      pendingEdits.current = remaining;
      if (remaining.length === 0) clearPendingEdits(session ?? null);
      else savePendingEdits(session ?? null, remaining);
      syncPendingCount();
      const tail =
        remaining.length > 0
          ? `  (${t("app.blocksStillPending", { count: remaining.length })})`
          : t("app.nothingWritten");
      return t("app.discardedCount", { count: selected.length }) + tail;
    },
    [session, syncPendingCount, pendingEdits],
  );

  return { codeApply, codeDiscard };
}
