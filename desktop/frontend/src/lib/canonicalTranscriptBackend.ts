import type { HistoryWindowPage, PersistentMessage } from "../generated/desktopContract.generated";
import { asArray } from "./array";
import { app } from "./bridge";
import { HistoryPreparingError } from "./historyPreparation";
import { canonicalUserDisplay } from "./canonicalUserDisplay";
import type { HistoryContentChunk, HistoryContentRef, HistoryEntry, HistoryMessage, HistorySlice, HistorySliceRequest, HistoryWindowPageView, HistoryWindowRequestView, MemoryCitation } from "./types";

const contentRecovery = new Map<string, () => void>();
export function registerTranscriptContentRecovery(tabId: string, recover: () => void): () => void {
  contentRecovery.set(tabId, recover);
  return () => { if (contentRecovery.get(tabId) === recover) contentRecovery.delete(tabId); };
}

function asWireObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

export function canonicalMessage(message: PersistentMessage, body: unknown): HistoryMessage {
  const raw = asWireObject(body);
  const decisionReceipt = asWireObject(raw.decision_receipt);
  if (Object.keys(decisionReceipt).length > 0) {
    return { role: "notice", messageId: String(raw.id ?? message.messageId), content: "", code: "decision_receipt", level: "info", decisionReceipt: decisionReceipt as unknown as HistoryMessage["decisionReceipt"] };
  }
  const readPause = asWireObject(raw.read_pause);
  if (Boolean(raw.local_only) && Object.keys(readPause).length > 0) {
    return { role: "notice", messageId: String(raw.id ?? message.messageId), content: "", code: "incomplete_read", level: "info", readPause: readPause as unknown as HistoryMessage["readPause"] };
  }
  const readiness = asWireObject(raw.final_readiness_recovery);
  if (Boolean(raw.local_only) && readiness.pending === true) {
    return { role: "notice", messageId: String(raw.id ?? message.messageId), content: "Final checks are still required before this task is complete.", code: "historical_checks", level: "info", readiness: { missing: Array.isArray(readiness.missing) ? readiness.missing.map(String) : undefined } };
  }
  const protocolRecovery = asWireObject(raw.protocol_recovery);
  if (Boolean(raw.local_only) && protocolRecovery.state === "pending" && typeof protocolRecovery.id === "string") {
    return { role: "notice", messageId: String(raw.id ?? message.messageId), content: "", code: "protocol_recovery", level: "info", pending: true, protocolRecovery: { id: protocolRecovery.id } };
  }
  const toolCalls = (Array.isArray(raw.tool_calls) ? raw.tool_calls as Record<string, unknown>[] : []).map(call => ({
    id: String(call.id ?? ""), name: String(call.name ?? ""), arguments: String(call.arguments ?? ""),
    resolvedName: typeof call.resolved_name === "string" ? call.resolved_name : undefined,
    capabilityId: typeof call.capability_id === "string" ? call.capability_id : undefined,
    resolvedReadOnly: typeof call.resolved_read_only === "boolean" ? call.resolved_read_only : undefined,
    diff: typeof call.diff === "string" ? call.diff : undefined,
    added: typeof call.added === "number" ? call.added : undefined,
    removed: typeof call.removed === "number" ? call.removed : undefined,
  }));
  const presented = asWireObject(raw.presented_files);
  const role = Boolean(raw.local_only) ? "assistant" : String(raw.role ?? message.role);
  const display = role === "user" ? canonicalUserDisplay(raw, message.preview ?? "") : { role, content: String(raw.content ?? raw.raw_content ?? message.preview ?? "") };
  return {
    turnFinal: message.turnFinal ?? false,
    samplingCount: message.samplingCount ?? undefined,
    toolCount: message.toolCount ?? undefined,
    turnDurationMs: message.turnDurationMs,
    role: display.role,
    messageId: String(raw.id ?? message.messageId),
    content: display.content,
    reasoning: typeof raw.reasoning_content === "string" ? raw.reasoning_content : undefined,
    createdAt: typeof raw.createdAt === "number" ? raw.createdAt : undefined,
    workDurationMs: typeof raw.workDurationMs === "number" ? raw.workDurationMs : undefined,
    toolCalls: toolCalls.length > 0 ? toolCalls : undefined,
    toolCallId: typeof raw.tool_call_id === "string" ? raw.tool_call_id : undefined,
    toolName: typeof raw.name === "string" ? raw.name : undefined,
    memoryCitations: Array.isArray(raw.memoryCitations) ? raw.memoryCitations as MemoryCitation[] : undefined,
    serverSearch: Array.isArray(raw.server_search) ? raw.server_search as HistoryMessage["serverSearch"] : undefined,
    execution: Object.keys(asWireObject(raw.tool_execution)).length > 0 ? raw.tool_execution as HistoryMessage["execution"] : undefined,
    presentedFiles: Array.isArray(presented.files) ? presented.files as HistoryMessage["presentedFiles"] : undefined,
    readCompletion: Object.keys(asWireObject(raw.read_completion)).length > 0 ? raw.read_completion as HistoryMessage["readCompletion"] : undefined,
  };
}

export function resolvedHistoryField(message: HistoryMessage, field: string): string | undefined {
  switch (field) {
    case "content": return message.content;
    case "reasoning": return message.reasoning;
    case "submitText": return message.submitText;
    case "detail": return message.detail;
    case "code": return message.code;
    case "summary": return message.summary;
    case "archive": return message.archive;
    case "toolResultError": return message.toolResultError;
    default: return message.content;
  }
}

// ── binding identity ────────────────────────────────────────────────────────
// A tab's history comes from exactly one place: the host that owns its
// binding. Crossing over on a failed call would let a transient local error
// (busy, conflict, timeout) be answered by a different service holding
// different data, so routing is decided by identity before the request, never
// by the outcome of one.
export type TranscriptBindingIdentity = "local" | "remote";

let bindingIdentityFor: ((tabId: string) => TranscriptBindingIdentity) | undefined;

/** Installed by the app layer, which is where tab metadata lives. */
export function setTranscriptBindingIdentity(resolver: (tabId: string) => TranscriptBindingIdentity): void {
  bindingIdentityFor = resolver;
}

// An unregistered identity is a tab with no remote binding, which is what a
// local session is. This is a default, not a fallback: it never moves a
// request to the other service because the first one answered badly.
function identityFor(tabId: string): TranscriptBindingIdentity {
  try {
    return bindingIdentityFor?.(tabId) ?? "local";
  } catch {
    return "local";
  }
}

export function entriesFor(messages: PersistentMessage[], snapshotSequence: number): HistoryEntry[] {
  return messages.map(persistent => {
    const entryId = `m:${persistent.messageId}`;
    return {
      entryId, turn: persistent.visibleTurn ?? 0, order: persistent.position,
      message: canonicalMessage(persistent, persistent.inline),
      refs: persistent.contentRef ? [{
        entryId, field: "canonicalMessage", size: persistent.contentRef.bytes,
        chunks: Math.max(1, Math.ceil(persistent.contentRef.bytes / (1 << 20))),
        revision: snapshotSequence, revKnown: true, digest: persistent.contentRef.digest,
        canonicalRef: persistent.contentRef,
      }] : [],
    };
  });
}

function unsupportedWindow(): HistoryWindowPageView {
  return {
    entries: [], status: "unsupported", olderCursor: "", newerCursor: "",
    hasOlder: false, hasNewer: false, totalTurns: 0, startTurn: 0, endTurn: 0,
    revision: 0, revisionKnown: false, digest: "",
  };
}

export async function canonicalHistoryWindow(tabId: string, req: HistoryWindowRequestView): Promise<HistoryWindowPageView> {
  const remote = identityFor(tabId) === "remote";
  let page: HistoryWindowPage;
  if (remote) {
    if (typeof app.RemoteSessionHistoryWindowForTab !== "function") {
      return unsupportedWindow();
    }
    page = await app.RemoteSessionHistoryWindowForTab(tabId, req);
  } else {
    if (typeof app.SessionHistoryWindowForTab !== "function") {
      return unsupportedWindow();
    }
    page = await app.SessionHistoryWindowForTab(tabId, req);
  }
  const status = (page.status || "ready") as HistoryWindowPageView["status"];
  if (status === "unsupported") {
    return unsupportedWindow();
  }
  const entries = entriesFor(asArray<PersistentMessage>(page.messages), page.snapshotSequence);
  const turns = entries.map(entry => entry.turn).filter(turn => turn > 0);
  return {
    entries,
    status,
    olderCursor: page.olderCursor ?? "",
    newerCursor: page.newerCursor ?? "",
    hasOlder: Boolean(page.hasOlder),
    hasNewer: Boolean(page.hasNewer),
    totalTurns: page.totalTurns ?? (turns.length > 0 ? Math.max(...turns) : 0),
    startTurn: turns.length > 0 ? Math.min(...turns) : 0,
    endTurn: turns.length > 0 ? Math.max(...turns) : 0,
    revision: page.snapshotSequence ?? 0,
    revisionKnown: (page.snapshotSequence ?? 0) > 0,
    digest: page.generation ?? "",
  };
}

function staleSlice(): HistorySlice {
  return { entries: [], nextCursor: "", hasOlder: false, hasNewer: false, newerCursor: "", totalTurns: 0, startTurn: 0, endTurn: 0, stale: true, revision: 0 };
}

/** A window page in the page-shaped form the resident store already consumes. */
function sliceFromWindow(window: HistoryWindowPageView, source: string): HistorySlice {
  return {
    entries: window.entries,
    nextCursor: window.olderCursor,
    hasOlder: window.hasOlder,
    newerCursor: window.newerCursor,
    hasNewer: window.hasNewer,
    totalTurns: window.totalTurns,
    startTurn: window.startTurn,
    endTurn: window.endTurn,
    stale: false,
    revision: window.revision,
    revisionKnown: window.revisionKnown,
    digest: window.digest,
    source,
  };
}

// turnWindowStatus maps a window status onto the page contract the store
// already understands. Empty pages alone are never an error, and a stale
// cursor is an answer rather than a failure.
function requireReadyWindow(window: HistoryWindowPageView): HistorySlice | undefined {
  switch (window.status) {
    case "ready": return sliceFromWindow(window, "window");
    case "stale_cursor": return staleSlice();
    case "preparing": throw new HistoryPreparingError();
    case "failed": throw new Error("Session history is failed");
    case "not_found": throw new Error("Session history is unavailable for this session");
    default: return undefined;
  }
}

export async function canonicalHistorySlice(tabId: string, req: HistorySliceRequest): Promise<HistorySlice> {
  const cursor = req.cursor ?? "";
  const limit = Math.min(100, Math.max(1, req.entries ?? 32));
  if (cursor.startsWith("reasonix:message:")) {
    const [id, sequence, generation] = cursor.slice("reasonix:message:".length).split(":");
    const messageId = decodeURIComponent(id);
    const snapshotSequence = Number(sequence);
    if (!Number.isSafeInteger(snapshotSequence) || snapshotSequence < 0 || generation === undefined) return staleSlice();
    const window = await canonicalHistoryWindow(tabId, { anchor: "message", messageId, snapshotSequence,
      generation: decodeURIComponent(generation), direction: req.newer ? "newer" : "older", limit });
    const ready = requireReadyWindow(window);
    if (!ready) throw new Error("Transcript v2 requires an updated Desktop and Serve");
    return ready;
  }
  // A cursor names a position inside a fixed canonical history window.
  if (cursor !== "" || req.newer) {
    const window = await canonicalHistoryWindow(tabId, {
      anchor: "cursor",
      cursor,
      direction: req.newer ? "newer" : "older",
      limit,
    });
    const ready = requireReadyWindow(window);
    if (ready) return ready;
    throw new Error("Transcript v2 requires an updated Desktop and Serve");
  }
  // The newest page carries the cursor for the same bidirectional window.
  const window = await canonicalHistoryWindow(tabId, { anchor: "newest", direction: "older", limit });
  const ready = requireReadyWindow(window);
  if (ready) return { ...ready, source: "recent" };
  throw new Error("Transcript v2 requires an updated Desktop and Serve");
}

function decodeBase64Bytes(data: string): Uint8Array {
  const binary = atob(data);
  return Uint8Array.from(binary, character => character.charCodeAt(0));
}

export async function canonicalHistoryContent(tabID: string, ref: HistoryContentRef, chunkIndex: number): Promise<HistoryContentChunk> {
  if (ref.transcriptRef) {
    const recover = contentRecovery.get(tabID);
    const read = identityFor(tabID) === "remote" ? app.RemoteTranscriptContentForTab : app.TranscriptContentForTab;
    if (!read) throw new Error("Transcript v2 content is unavailable");
    let offset = 0, data = "";
    while (true) {
      const chunk = await read(tabID, { ...ref.transcriptRef, offset });
      if (chunk.stale) {
        if (contentRecovery.get(tabID) === recover) recover?.();
        throw new Error("Transcript content snapshot expired; synchronizing, retry after recovery");
      }
      data += chunk.data;
      if (chunk.done) return { entryId: ref.entryId, field: ref.field, chunk: chunkIndex, chunks: 1, data, done: true, stale: false };
      if (chunk.nextOffset <= offset) throw new Error("Transcript content did not advance");
      offset = chunk.nextOffset;
    }
  }
  if (!ref.canonicalRef) return app.HistoryContentForTab(tabID, ref, chunkIndex);
  const offset = chunkIndex * (1 << 20);
  const chunk = identityFor(tabID) === "remote"
    ? await app.RemoteSessionHistoryContentForTab(tabID, ref.canonicalRef, offset)
    : await app.SessionHistoryContentForTab(tabID, ref.canonicalRef, offset);
  const bytes = decodeBase64Bytes(chunk.data ?? "");
  let data = "";
  for (let start = 0; start < bytes.length; start += 0x8000) data += String.fromCharCode(...bytes.subarray(start, start + 0x8000));
  return { entryId: ref.entryId, field: ref.field, chunk: chunkIndex, chunks: ref.chunks, data, done: chunk.done, stale: false };
}
