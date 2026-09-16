import type {
  HistoryWindowPage,
  HistoryWindowRequest,
  MessageFieldPage,
  MessageHistoryPage,
  MessageLocation,
  PersistentMessage,
  Ref as SessionContentRef,
  SearchHistoryPage,
  SessionHistoryContentChunk,
  SessionOpenView,
  FollowRequest, TranscriptFollowResponse, Change,
} from "../generated/desktopContract.generated";
import type { HistoryContentChunk, HistoryContentRef, HistoryMessage, HistorySlice, HistorySliceRequest, WireEvent } from "./types";

export interface SessionReaderBindings {
  TranscriptFollowForTab(tabID: string, request: FollowRequest): Promise<TranscriptFollowResponse>;
  RemoteTranscriptFollowForTab(tabID: string, request: FollowRequest): Promise<TranscriptFollowResponse>;
  SessionHistoryPageForTab(tabID: string, cursor: string, limit: number): Promise<MessageHistoryPage>;
  SessionOpenForTab(tabID: string): Promise<SessionOpenView>;
  SessionHistoryContentForTab(tabID: string, ref: SessionContentRef, offset: number): Promise<SessionHistoryContentChunk>;
  RemoteSessionOpenForTab(tabID: string): Promise<SessionOpenView>;
  RemoteSessionHistoryPageForTab(tabID: string, cursor: string, limit: number): Promise<MessageHistoryPage>;
  RemoteSessionHistoryContentForTab(tabID: string, ref: SessionContentRef, offset: number): Promise<SessionHistoryContentChunk>;
  SearchSessionHistoryForTab(tabID: string, textQuery: string, cursor: string, limit: number): Promise<SearchHistoryPage>;
  RemoteSearchSessionHistoryForTab(tabID: string, textQuery: string, cursor: string, limit: number): Promise<SearchHistoryPage>;
  LocateSessionMessageForTab(tabID: string, messageID: string, snapshot: number): Promise<MessageLocation>;
  RemoteLocateSessionMessageForTab(tabID: string, messageID: string, snapshot: number): Promise<MessageLocation>;
  // history-window-v1. A binding that does not implement these (or a remote
  // service that does not advertise the capability) is read through the
  // protocol-7 adapter in canonicalTranscriptBackend instead.
  SessionHistoryWindowForTab(tabID: string, req: HistoryWindowRequest): Promise<HistoryWindowPage>;
  SessionMessageFieldForTab(tabID: string, messageID: string, version: number, field: string, offset: number, length: number): Promise<MessageFieldPage>;
  RemoteSessionHistoryWindowForTab(tabID: string, req: HistoryWindowRequest): Promise<HistoryWindowPage>;
  RemoteSessionMessageFieldForTab(tabID: string, messageID: string, version: number, field: string, offset: number, length: number): Promise<MessageFieldPage>;
}

interface MockSessionReaderHost {
  MetaForTab?(tabID: string): Promise<{ running?: boolean; runtime?: { epoch?: string } }>;
  HistoryForTab?(tabID: string): Promise<HistoryMessage[]>;
  HistorySliceForTab(tabID: string, req: HistorySliceRequest): Promise<HistorySlice>;
  HistoryContentForTab(tabID: string, ref: HistoryContentRef, chunkIndex: number): Promise<HistoryContentChunk>;
  SessionHistoryPageForTab(tabID: string, cursor: string, limit: number): Promise<MessageHistoryPage>;
  SessionOpenForTab(tabID: string): Promise<SessionOpenView>;
  SessionHistoryContentForTab(tabID: string, ref: SessionContentRef, offset: number): Promise<SessionHistoryContentChunk>;
  SessionHistoryWindowForTab(tabID: string, req: HistoryWindowRequest): Promise<HistoryWindowPage>;
}

async function mockReaderSlice(host: MockSessionReaderHost, tab: string, request: HistorySliceRequest): Promise<HistorySlice> {
  if (typeof host.HistorySliceForTab === "function") return host.HistorySliceForTab(tab, request);
  const messages = await host.HistoryForTab?.(tab) ?? [];
  let turn = 0;
  const entries = messages.map((message, order) => {
    if (message.role === "user") turn++;
    return { entryId: message.messageId ?? `mock:${tab}:${order}`, order, turn, message, refs: [] };
  }).slice(-(request.entries ?? 32));
  return { entries, totalTurns: turn, startTurn: entries[0]?.turn ?? 0, endTurn: turn, nextCursor: "",
    hasOlder: false, hasNewer: false, revision: 1, revisionKnown: true, digest: `mock:${tab}`, stale: false };
}

// Test/browser host implementation of the mandatory protocol. Production
// transports never route an unsupported Follow request through this adapter.
const mockFollowers = new Map<string, { tab: string; epoch?: string; revision: number; coverage: number; queue: Change[]; wake?: () => void }>();
const mockPrompts = new Map<string, Map<string, WireEvent>>();
const mockActive = new Map<string, { id: string; text: string; reasoning: string }>();
const mockMetadata = new Map<string, { turnId?: string; running?: boolean; runtime?: { epoch?: string } }>();
export function setMockTranscriptMetadata(tab: string, meta: { turnId?: string; running?: boolean; runtime?: { epoch?: string } }): void {
  const prior = mockMetadata.get(tab)?.runtime?.epoch;
  if (prior && meta.runtime?.epoch && prior !== meta.runtime.epoch) { mockActive.delete(tab); mockPrompts.delete(tab); }
  mockMetadata.set(tab, { ...mockMetadata.get(tab), ...meta });
}
let mockSubscription = 0;
let mockMessage = 0;
export function publishMockTranscriptEvent(event: WireEvent): void {
  if (event.tabId) {
    const tab = event.tabId;
    const epoch = mockMetadata.get(tab)?.runtime?.epoch;
    if (event.runtimeEpoch && epoch && event.runtimeEpoch !== epoch) return;
    if (event.kind === "turn_started") setMockTranscriptMetadata(tab, { running: true });
    if (event.kind === "stream_attempt" && event.streamAttempt?.action === "begin" && event.messageId) {
      mockActive.set(tab, { id: event.messageId, text: "", reasoning: "" });
    }
    if (event.kind === "text" || event.kind === "reasoning") {
      const active = mockActive.get(tab) ?? { id: event.messageId ?? `mock-active:${tab}:${++mockMessage}`, text: "", reasoning: "" };
      if (event.kind === "text") active.text += event.text ?? ""; else active.reasoning += event.text ?? "";
      mockActive.set(tab, active);
      event = { ...event, messageId: active.id, attemptId: active.id };
      setMockTranscriptMetadata(tab, { running: true });
    }
    if (event.kind === "turn_done") { mockActive.delete(tab); setMockTranscriptMetadata(tab, { running: false }); }
    const prompts = mockPrompts.get(tab) ?? new Map<string, WireEvent>();
    const id = event.approval?.id ?? event.ask?.id;
    if ((event.kind === "approval_request" || event.kind === "ask_request") && id) prompts.set(id, event);
    if (event.kind === "prompt_answered" && event.itemId) prompts.delete(event.itemId);
    if (event.kind === "turn_done") prompts.clear();
    mockPrompts.set(tab, prompts);
  }
  for (const follower of mockFollowers.values()) {
    if (event.tabId && event.tabId !== follower.tab) continue;
    if (event.runtimeEpoch && follower.epoch && event.runtimeEpoch !== follower.epoch) continue;
    follower.queue.push({ revision: ++follower.revision, commitSeq: follower.coverage, durableSeq: follower.coverage,
      index: 0, event: { ...event, seq: undefined } as unknown as Change["event"] });
    follower.wake?.();
  }
}

function messageIndex(entryId: string): number {
  return Number(/:m(\d+):o\d+$/.exec(entryId)?.[1] ?? -1);
}

function canonicalBody(message: HistoryMessage | undefined, messageId: string): Uint8Array {
  if (!message) return new Uint8Array();
  const body = {
    ...message,
    id: message.messageId ?? messageId,
    reasoning_content: message.reasoning,
    tool_calls: message.toolCalls?.map(call => ({
      ...call,
      resolved_name: call.resolvedName,
      capability_id: call.capabilityId,
      resolved_read_only: call.resolvedReadOnly,
    })),
    tool_call_id: message.toolCallId,
    name: message.toolName,
    tool_execution: message.execution,
    presented_files: message.presentedFiles ? { files: message.presentedFiles } : undefined,
    server_search: message.serverSearch,
  };
  return new TextEncoder().encode(JSON.stringify(body));
}

function persistentMessages(slice: HistorySlice, history: HistoryMessage[]): PersistentMessage[] {
  return slice.entries.map(entry => {
    const index = messageIndex(entry.entryId);
    const messageId = entry.message.messageId ?? entry.entryId;
    const body = canonicalBody(history[index], messageId);
    return {
      messageId, position: entry.order,
      version: 1, role: entry.message.role, preview: entry.message.content ?? "",
      eventSequence: 0, visibleTurn: entry.turn, inline: JSON.parse(new TextDecoder().decode(canonicalBody(entry.message, messageId))),
      contentRef: (entry.refs ?? []).length > 0 ? { digest: `mock-canonical:${index}`, bytes: body.length, mediaType: "application/json" } : undefined,
    };
  });
}

function encodedChunk(bytes: Uint8Array): string {
  let binary = "";
  for (let start = 0; start < bytes.length; start += 0x8000) binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000));
  return btoa(binary);
}

export function makeMockSessionReaderBindings(): SessionReaderBindings {
  const search = (): SearchHistoryPage => ({ hits: [], snapshotSequence: 0, coverageSequence: 0, status: "preparing", hasMore: false });
  async function follow(this: MockSessionReaderHost, tab: string, request: FollowRequest): Promise<TranscriptFollowResponse> {
    const response: TranscriptFollowResponse = { protocolVersion: 2, subscription: request.subscription ?? "", changes: [], resetRequired: false };
    if (request.close) { mockFollowers.get(response.subscription)?.wake?.(); mockFollowers.delete(response.subscription); return response; }
    if (!request.subscription) {
      const id = `mock-follow-${++mockSubscription}`;
      const follower = { tab, epoch: undefined as string | undefined, revision: 1, coverage: 0, queue: [] as Change[] };
      mockFollowers.set(id, follower);
      const meta = mockMetadata.get(tab);
      const active = mockActive.get(tab) ? { ...mockActive.get(tab)! } : undefined;
      const pendingEvents = [...(mockPrompts.get(tab)?.values() ?? [])];
      follower.epoch = meta?.runtime?.epoch;
      const page = await (this.SessionHistoryWindowForTab ?? bindings.SessionHistoryWindowForTab).call(this, tab, { anchor: "newest", limit: 32 });
      follower.coverage = page.snapshotSequence;
      follower.queue = follower.queue.map(change => ({ ...change, commitSeq: page.snapshotSequence, durableSeq: page.snapshotSequence }));
      return { ...response, subscription: id, history: page, snapshot: {
        protocolVersion: 2, snapshotId: id, identity: { sessionId: tab, runtimeEpoch: meta?.runtime?.epoch ?? "mock-runtime", headId: "", rewriteEpoch: 0 },
        projectionRevision: 1, coveredThroughSeq: page.snapshotSequence, durableSeq: page.snapshotSequence,
        records: [], activeRecords: active ? [{ id: `m:${active.id}`, order: page.messages.length, message: { role: "assistant", messageId: active.id, content: active.text, reasoning: active.reasoning }, refs: [] }] : [],
        activeAttempts: active ? [{ id: active.id, messageId: active.id, turnId: "mock-turn", nextIndex: 0 }] : [], totalRecords: page.messages.length + (active ? 1 : 0), totalTurns: page.totalTurns,
        before: 0, hasOlder: page.hasOlder, stale: false,
        runtime: { turnId: meta?.turnId, status: pendingEvents.length ? "waiting_user" : meta?.running ? "in_progress" : "completed", pendingEvents: pendingEvents as unknown as NonNullable<TranscriptFollowResponse["snapshot"]>["runtime"]["pendingEvents"], samplingCount: 0, toolCount: 0 },
      } };
    }
    const follower = mockFollowers.get(request.subscription);
    if (!follower) return { ...response, resetRequired: true };
    follower.queue = follower.queue.filter(change => change.revision > (request.afterRevision ?? 0));
    if (!follower.queue.length) await new Promise<void>(resolve => { follower.wake = resolve; });
    follower.wake = undefined;
    return { ...response, changes: [...follower.queue] };
  }
  const bindings: SessionReaderBindings = {
    TranscriptFollowForTab: follow,
    RemoteTranscriptFollowForTab: follow,
    async SessionHistoryPageForTab(this: MockSessionReaderHost, tabID, cursor, limit) {
      const slice = await mockReaderSlice(this, tabID, { cursor, entries: limit, turns: limit });
      const history = slice.entries.some(entry => entry.refs?.length) ? await this.HistoryForTab?.(tabID) ?? [] : [];
      return { messages: persistentMessages(slice, history), snapshotSequence: slice.revision, coverageSequence: slice.revision, status: "ready", totalTurns: slice.totalTurns, generation: slice.digest ?? "", nextCursor: slice.nextCursor, hasMore: slice.hasOlder };
    },
    async SessionOpenForTab(this: MockSessionReaderHost, tabID) {
      const slice = await mockReaderSlice(this, tabID, { cursor: "", entries: 100, turns: 100 });
      const history = slice.entries.some(entry => entry.refs?.length) ? await this.HistoryForTab?.(tabID) ?? [] : [];
      const entries = persistentMessages(slice, history);
      return { session: { hostId: "local", sessionId: tabID }, storageGeneration: slice.digest, snapshotSequence: slice.revision, acceptedSequence: slice.revision, durableSequence: slice.revision, recent: { version: 1, sessionId: tabID, storageGeneration: slice.digest ?? "", durableSequence: slice.revision, totalTurns: slice.totalTurns, entries }, recovery: "ready", history: "ready", search: "preparing", canExecute: true };
    },
    async SessionHistoryContentForTab(this: MockSessionReaderHost, tabID, ref, offset) {
      const index = Number(ref.digest.replace("mock-canonical:", ""));
      const history = await this.HistoryForTab?.(tabID) ?? [];
      const message = history[index];
      const entryId = `smock-${tabID}:r0:m${index}:o0`;
      await this.HistoryContentForTab(tabID, { entryId, field: "content", size: message?.content?.length ?? 0, chunks: 1, revision: 0, digest: "mock" }, 0);
      const body = canonicalBody(message, entryId);
      const nextOffset = Math.min(body.length, offset + (1 << 20));
      return { data: encodedChunk(body.subarray(offset, nextOffset)), nextOffset, done: nextOffset >= body.length };
    },
    async RemoteSessionOpenForTab(this: MockSessionReaderHost, tabID) { return this.SessionOpenForTab(tabID); },
    async RemoteSessionHistoryPageForTab(this: MockSessionReaderHost, tabID, cursor, limit) { return this.SessionHistoryPageForTab(tabID, cursor, limit); },
    async RemoteSessionHistoryContentForTab(this: MockSessionReaderHost, tabID, ref, offset) { return this.SessionHistoryContentForTab(tabID, ref, offset); },
    async SearchSessionHistoryForTab() { return search(); },
    async RemoteSearchSessionHistoryForTab() { return search(); },
    async LocateSessionMessageForTab(_tabID, messageID) { return { status: "not_found", messageId: messageID, snapshotSequence: 0, coverageSequence: 0 }; },
    async RemoteLocateSessionMessageForTab(_tabID, messageID) { return { status: "not_found", messageId: messageID, snapshotSequence: 0, coverageSequence: 0 }; },
    // The in-memory mock has one direction of history: a newest page and its
    // older cursors. It answers a window request without inventing a newer
    // cursor, which is exactly how a protocol-7 service behaves.
    async SessionHistoryWindowForTab(this: MockSessionReaderHost, tabID, req) {
      const slice = await mockReaderSlice(this, tabID, {
        cursor: req.anchor === "cursor" ? req.cursor ?? "" : "",
        entries: req.limit,
        turns: req.limit,
      });
      const history = slice.entries.some(entry => entry.refs?.length) ? await this.HistoryForTab?.(tabID) ?? [] : [];
      return {
        messages: persistentMessages(slice, history),
        status: slice.stale ? "stale_cursor" : "ready",
        snapshotSequence: slice.revision,
        coverageSequence: slice.revision,
        generation: slice.digest,
        totalTurns: slice.totalTurns,
        hasOlder: slice.hasOlder,
        // The in-memory mock has one direction of history, exactly like a
        // protocol-7 service: it never invents a newer cursor.
        hasNewer: false,
        olderCursor: slice.nextCursor,
        newerCursor: "",
      };
    },
    async SessionMessageFieldForTab() { return { status: "not_found", messageId: "", version: 0, field: "", totalBytes: 0, offset: 0, encoding: "utf-8" }; },
    async RemoteSessionHistoryWindowForTab(this: MockSessionReaderHost, tabID, req) { return this.SessionHistoryWindowForTab(tabID, req); },
    async RemoteSessionMessageFieldForTab() { return { status: "not_found", messageId: "", version: 0, field: "", totalBytes: 0, offset: 0, encoding: "utf-8" }; },
  };
  return bindings;
}
