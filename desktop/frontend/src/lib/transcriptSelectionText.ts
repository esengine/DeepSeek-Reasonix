import { parseAttachmentRefsForDisplay } from "./attachmentDisplay";
import { contentRevision } from "./contentRevision";
import { estimateHastBytes } from "./markdownByteEstimate";
import { stripMemoryCompilerExecution } from "./memoryCompilerDisplay";
import { parseSelectedTextContext, stripSelectionLabels } from "./selectedTextContext";
import { getMarkdownWorkerClient } from "./markdownWorkerClient";
import { getTranscriptStore } from "./transcriptStore";
import { historyEntryIdForItemId, type TranscriptRow } from "./transcriptRows";
import type { LiveStream } from "./useController";
import type { TranscriptSelectableRow } from "./transcriptSelectionStore";

const IM_SOURCE_START = "[[reasonix-im]]";
const IM_SOURCE_END = "[[/reasonix-im]]";

function imMessageBody(text: string): string {
  if (!text.startsWith(IM_SOURCE_START)) return text;
  const end = text.indexOf(IM_SOURCE_END);
  return end < 0 ? text : text.slice(end + IM_SOURCE_END.length).replace(/^\r?\n/, "");
}

export function userMessageSelectionText(text: string, submitText?: string): string {
  const actionText = stripMemoryCompilerExecution(imMessageBody(text));
  const selected = parseSelectedTextContext(submitText);
  const withoutLabels = stripSelectionLabels(actionText, selected);
  const body = parseAttachmentRefsForDisplay(withoutLabels).text.trim();
  return [body, ...selected.map((entry) => entry.text.trim())].filter(Boolean).join("\n\n");
}

async function markdownSelectionText(sourceText: string, entryId?: string): Promise<string> {
  const revision = contentRevision(sourceText);
  const store = getTranscriptStore();
  const cached = entryId ? store.getMarkdown(entryId, revision) : undefined;
  if (cached?.source === sourceText) return cached.selectionText;

  try {
    const result = await getMarkdownWorkerClient().parse(sourceText).promise;
    if (!result) return sourceText;
    if (entryId) {
      const bytes = sourceText.length * 2 + result.selectionText.length * 2 + estimateHastBytes(result.blocks);
      store.setMarkdown(entryId, revision, { source: sourceText, ...result, bytes });
    }
    return result.selectionText;
  } catch {
    try {
      const pipeline = await import("./markdownPipeline");
      return pipeline.parseMarkdown(sourceText).selectionText;
    } catch {
      return sourceText;
    }
  }
}

function markdownRow(rowKey: string, sourceText: string, entryId?: string): TranscriptSelectableRow {
  const revision = contentRevision(sourceText);
  return {
    rowKey,
    sourceText,
    contentRevision: revision,
    resolveText: () => markdownSelectionText(sourceText, entryId),
    pin: entryId ? () => getTranscriptStore().pinMarkdown(entryId, revision) : undefined,
  };
}

/** Stable readable projections for the structural transcript row model. */
export function transcriptSelectableRows(
  rows: readonly TranscriptRow[],
): TranscriptSelectableRow[] {
  const selectable: TranscriptSelectableRow[] = [];
  for (const row of rows) {
    const rowKey = String(row.key);
    if (row.kind === "user") {
      const sourceText = userMessageSelectionText(row.item.text, row.item.submitText);
      selectable.push({
        rowKey,
        sourceText,
        contentRevision: contentRevision(`${row.item.text}\u0000${row.item.submitText ?? ""}`),
        resolveText: async () => sourceText,
      });
      continue;
    }
    if (row.kind === "answer") {
      selectable.push(markdownRow(rowKey, row.item.text, historyEntryIdForItemId(row.item.id)));
      continue;
    }
    if (row.kind === "reasoning") {
      selectable.push({
        ...markdownRow(rowKey, row.item.reasoning, historyEntryIdForItemId(row.item.id)),
        kind: "reasoning",
      });
    }
  }
  return selectable;
}

/**
 * Project only the active stream rows. Token updates therefore hash at most
 * the answer and reasoning bodies instead of every loaded history row.
 */
export function transcriptLiveSelectableRows(
  rowsByKey: ReadonlyMap<string, TranscriptSelectableRow>,
  live?: LiveStream,
): TranscriptSelectableRow[] {
  if (!live) return [];
  const rows: TranscriptSelectableRow[] = [];
  const answerKey = `a:${live.id}`;
  if (rowsByKey.has(answerKey)) rows.push(markdownRow(answerKey, live.text));
  const reasoningKey = `r:${live.id}`;
  if (rowsByKey.has(reasoningKey)) {
    rows.push({ ...markdownRow(reasoningKey, live.reasoning), kind: "reasoning" });
  }
  return rows;
}

/** Materialize the current freeze snapshot only when selection needs it. */
export function mergeTranscriptSelectableRows(
  rows: readonly TranscriptSelectableRow[],
  overrides: readonly TranscriptSelectableRow[],
): readonly TranscriptSelectableRow[] {
  if (overrides.length === 0) return rows;
  return rows.map((row) => overrides.find((candidate) => candidate.rowKey === row.rowKey) ?? row);
}
