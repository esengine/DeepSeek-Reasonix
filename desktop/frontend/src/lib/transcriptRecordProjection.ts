import { historyToolStatus } from "./historyToolStatus";
import { asArray } from "./array";
import { canonicalMessage } from "./canonicalTranscriptBackend";
import { historicalResultNotice } from "./completionResultState";
import { historyNoticeItems } from "./controllerNotices";
import { appendHistoryAttachmentRefs } from "./historyAttachmentRefs";
import { historySearchAndAnswer } from "./searchTranscript";
import { fileDiffFromWire, summarizeFileDiff } from "./tools";
import { historyToolError, isReadOnlyTool, type Item } from "./useController";
import type { HistoryContentRef, HistoryEntry, HistoryMessage, MemoryCitation } from "./types";
import { recordBytes } from "./transcriptRecordBytes";

export interface TranscriptRecord {
  entryId: string;
  turn: number;
  order: number;
  message: HistoryMessage;
  refs: HistoryContentRef[];
  resolved?: Record<string, string>;
  staleRefs?: Record<string, true>;
  bytes: number;
}

export interface RecordConversion {
  items: Item[];
  claims: string[];
  unresolvedIds: string[];
  pendingPositional: number[];
  matches: Map<number, string>;
}

export function entryToRecord(entry: HistoryEntry): TranscriptRecord {
  return {
    entryId: entry.entryId,
    turn: entry.turn,
    order: entry.order,
    message: entry.message,
    refs: asArray<HistoryContentRef>(entry.refs),
    bytes: recordBytes(entry.message),
  };
}

export function itemIdForToolCall(toolCallId: string, fallback: string): string {
  return toolCallId || fallback;
}

/** Converts one record against the complete resident window. */
export function convertRecord(
  rec: TranscriptRecord,
  view: { records: TranscriptRecord[]; indexOf: Map<string, number>; toolResultOwners: Map<string, string> },
  consumed: Set<string>,
  priorMatches?: Map<number, string>,
): RecordConversion {
  const converted = convertRecordBody(rec, view, consumed, priorMatches);
  if (rec.message.turnId) converted.items = converted.items.map(item => ({ ...item, turnId: rec.message.turnId }));
  return converted;
}

function convertRecordBody(
  rec: TranscriptRecord,
  view: { records: TranscriptRecord[]; indexOf: Map<string, number>; toolResultOwners: Map<string, string> },
  consumed: Set<string>,
  priorMatches?: Map<number, string>,
): RecordConversion {
  const items: Item[] = [];
  const claims: string[] = [];
  const unresolvedIds: string[] = [];
  const pendingPositional: number[] = [];
  const matches = new Map<number, string>(priorMatches);
  const message = rec.message;
  const id = message.messageId && (message.role === "assistant" || message.role === "user") ? `m:${message.messageId}` : `he:${rec.entryId}`;

  if (message.role === "system") return { items, claims, unresolvedIds, pendingPositional, matches };
  if (message.role === "phase") {
    if (message.content.trim() !== "") items.push({ kind: "phase", id, text: message.content });
    return { items, claims, unresolvedIds, pendingPositional, matches };
  }
  if (message.role === "notice") {
    if (message.completionReceipt || message.completionSummary) {
      const result = historicalResultNotice(message, id);
      if (result) items.push(result);
      return { items, claims, unresolvedIds, pendingPositional, matches };
    }
    return { items: historyNoticeItems(message, id), claims, unresolvedIds, pendingPositional, matches };
  }
  if (message.role === "compaction") {
    items.push({
      kind: "compaction", id, pending: Boolean(message.pending), trigger: message.trigger ?? "",
      messages: message.messages ?? 0, summary: message.summary ?? "", archive: message.archive ?? "",
    });
    return { items, claims, unresolvedIds, pendingPositional, matches };
  }
  if (message.role === "user") {
    if (message.content.trim() !== "") {
      items.push({ kind: "user", id, messageId: message.messageId, submissionId: message.submissionId,
        text: appendHistoryAttachmentRefs(message.content, message.attachments), submitText: message.submitText, createdAt: message.createdAt,
        checkpointTurn: message.checkpointTurn, historyTurn: rec.turn > 0 ? rec.turn : undefined });
    }
    return { items, claims, unresolvedIds, pendingPositional, matches };
  }
  if (message.role === "assistant") {
    const memoryCitations = asArray<MemoryCitation>(message.memoryCitations);
    items.push(...historySearchAndAnswer(id, {
      content: message.content, reasoning: message.reasoning, workDurationMs: message.workDurationMs,
      turnFinal: message.turnFinal, samplingCount: message.samplingCount, toolCount: message.toolCount, turnDurationMs: message.turnDurationMs, turnUsage: message.turnUsage, createdAt: message.createdAt,
      memoryCitations: memoryCitations.length > 0 ? memoryCitations : undefined, serverSearch: message.serverSearch,
    }, rec.refs.length > 0));
    const toolCalls = message.toolCalls ?? [];
    let scan = (view.indexOf.get(rec.entryId) ?? -1) + 1;
    for (let callIndex = 0; callIndex < toolCalls.length; callIndex += 1) {
      const toolCall = toolCalls[callIndex];
      let result: HistoryMessage | undefined;
      let resultEntryId: string | undefined;
      const prior = matches.get(callIndex);
      if (prior) {
        resultEntryId = prior;
        result = view.records[view.indexOf.get(prior) ?? -1]?.message;
      } else if (toolCall.id) {
        const owner = view.toolResultOwners.get(toolCall.id);
        if (owner) {
          resultEntryId = owner;
          result = view.records[view.indexOf.get(owner) ?? -1]?.message;
        } else unresolvedIds.push(toolCall.id);
      } else {
        while (scan < view.records.length) {
          const candidate = view.records[scan];
          if (candidate.message.role !== "tool") break;
          scan += 1;
          if (candidate.message.toolCallId || consumed.has(candidate.entryId)) continue;
          resultEntryId = candidate.entryId;
          result = candidate.message;
          break;
        }
        if (!resultEntryId) pendingPositional.push(callIndex);
      }
      if (resultEntryId) {
        matches.set(callIndex, resultEntryId);
        claims.push(resultEntryId);
        consumed.add(resultEntryId);
      }
      const archived = Boolean(toolCall.argumentsArchived || result?.toolResultArchived || (!result && toolCall.resultObservation?.contentRef));
      const output = result?.toolResultArchived ? undefined : result?.content ?? "";
      const error = result?.toolResultError || (output ? historyToolError(output) : undefined);
      const fileDiff = fileDiffFromWire(toolCall);
      items.push({
        kind: "tool", id: itemIdForToolCall(toolCall.id, `he:${rec.entryId}:tc${callIndex}`), name: toolCall.name,
        args: toolCall.arguments ?? "", readOnly: typeof toolCall.resolvedReadOnly === "boolean" ? toolCall.resolvedReadOnly : isReadOnlyTool(toolCall.name),
        resolvedName: toolCall.resolvedName, capabilityId: toolCall.capabilityId,
        status: historyToolStatus(result, toolCall, error), contentState: !result || archived ? "unloaded" : "ready", resultMissing: !result && !toolCall.resultObservation?.messageId || undefined, output, error, dataArchived: archived || undefined,
        subject: toolCall.subject, summary: summarizeFileDiff(fileDiff) || toolCall.summary, fileDiff,
        isShell: toolCall.name === "bash" || (toolCall.id || "").startsWith("shell-"), execution: result?.execution,
        presentedFiles: result?.presentedFiles,
      });
    }
    return { items, claims, unresolvedIds, pendingPositional, matches };
  }
  if (message.role === "tool") {
    if (consumed.has(rec.entryId)) return { items, claims, unresolvedIds, pendingPositional, matches };
    const output = message.toolResultArchived ? undefined : message.content;
    const error = message.toolResultError || (output ? historyToolError(output) : undefined);
    items.push({
      kind: "tool", id: itemIdForToolCall(message.toolCallId ?? "", id), name: message.toolName || "tool", args: "",
      readOnly: isReadOnlyTool(message.toolName || "tool"), status: error ? "error" : "done", output, error,
      dataArchived: message.toolResultArchived || undefined, isShell: (message.toolName || "") === "bash" || (message.toolCallId || "").startsWith("shell-"),
      execution: message.execution, presentedFiles: message.presentedFiles,
    });
  }
  return { items, claims, unresolvedIds, pendingPositional, matches };
}

export function applyResolvedField(rec: TranscriptRecord, ref: HistoryContentRef, data: string): boolean {
  const message = rec.message;
  switch (ref.field) {
    case "canonicalMessage": {
      const bytes = Uint8Array.from(data, character => character.charCodeAt(0));
      const decoded = canonicalMessage(
        { messageId: rec.entryId, submissionId: message.submissionId, position: rec.turn, version: 1, role: message.role, eventSequence: 0, visibleTurn: rec.turn, turnFinal: message.turnFinal, toolObservations: Object.fromEntries((message.toolCalls ?? []).flatMap(call => call.resultObservation ? [[call.id, call.resultObservation]] : [])), turnDurationMs: message.turnDurationMs, samplingCount: message.samplingCount, toolCount: message.toolCount },
        JSON.parse(new TextDecoder().decode(bytes)),
      );
      rec.message = { ...message, ...decoded };
      return true;
    }
    case "content": rec.message = { ...message, content: data }; return true;
    case "reasoning": rec.message = { ...message, reasoning: data }; return true;
    case "submitText": rec.message = { ...message, submitText: data }; return true;
    case "detail": rec.message = { ...message, detail: data }; return true;
    case "code": rec.message = { ...message, code: data }; return true;
    case "summary": rec.message = { ...message, summary: data }; return true;
    case "archive": rec.message = { ...message, archive: data }; return true;
    case "toolResultError": rec.message = { ...message, toolResultError: data }; return true;
    case "toolArguments":
    case "toolSubject":
    case "toolSummary":
    case "toolDiff": {
      rec.message = { ...message, toolCalls: (message.toolCalls ?? []).map((toolCall) => {
        if (toolCall.id !== ref.toolCallId) return toolCall;
        if (ref.field === "toolArguments") return { ...toolCall, arguments: data };
        if (ref.field === "toolSubject") return { ...toolCall, subject: data };
        if (ref.field === "toolSummary") return { ...toolCall, summary: data };
        return { ...toolCall, diff: data };
      }) };
      return true;
    }
    default: return false;
  }
}
