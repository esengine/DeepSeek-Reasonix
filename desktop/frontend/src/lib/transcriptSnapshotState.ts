import type { HistoryMessage, WireEvent } from "./types";
import type { Item, State } from "./useController";
import type { TranscriptRecord, TranscriptSnapshot } from "./transcriptProtocol";
import { canonicalUserConfirmations, settleLocalSubmissions } from "./localSubmissionState";

export function snapshotRecords(snapshot: TranscriptSnapshot): TranscriptRecord[] {
  if (!Number.isSafeInteger(snapshot.totalRecords) || snapshot.totalRecords < 0) {
    throw new Error("invalid transcript snapshot record count");
  }
  const records: TranscriptRecord[] = [];
  const indexes = new Map<string, number>();
  const normalize = (record: TranscriptRecord): TranscriptRecord => {
    if (!Number.isSafeInteger(record.order) || record.order < 0 || record.order >= snapshot.totalRecords) {
      throw new Error("invalid transcript snapshot record identity");
    }
    const derived = record.message.role === "tool" && record.message.toolCallId ? `tool:${record.message.toolCallId}`
      : record.message.messageId ? `m:${record.message.messageId}` : undefined;
    const referencedID = record.refs.find(ref => ref.recordId)?.recordId;
    let id = record.id || record.message.recordId || derived || referencedID;
    if (!id) {
      if (record.refs.length > 0) throw new Error("transcript snapshot content identity missing");
      id = `view:legacy:${snapshot.snapshotId || `${snapshot.identity.runtimeEpoch}:${snapshot.projectionRevision}`}:${record.order}`;
    }
    if ((record.message.recordId && record.message.recordId !== id) || (derived && derived !== id)) {
      throw new Error("transcript snapshot record identity mismatch");
    }
    if (record.refs.some(ref => ref.recordId !== id)) throw new Error("transcript snapshot content identity mismatch");
    return { ...record, id, message: { ...record.message, recordId: id } };
  };
  for (const raw of snapshot.records ?? []) {
    const record = normalize(raw);
    if (indexes.has(record.id)) throw new Error("duplicate transcript snapshot record identity");
    indexes.set(record.id, records.length);
    records.push(record);
  }
  const activeIDs = new Set<string>();
  for (const raw of snapshot.activeRecords ?? []) {
    const record = normalize(raw);
    if (activeIDs.has(record.id)) throw new Error("duplicate active transcript snapshot record identity");
    activeIDs.add(record.id);
    // A mutable owner may also fall inside the requested page. The backend
    // omits that duplicate, but tolerate older/remote implementations that
    // return it in both arrays and keep the active copy authoritative.
    const index = indexes.get(record.id);
    if (index !== undefined) records[index] = record;
    else { indexes.set(record.id, records.length); records.push(record); }
  }
  return records.sort((a, b) => a.order - b.order);
}

type Convert = (messages: HistoryMessage[], prefix: string) => { items: Item[]; seq: number };
type ApplyEvent = (state: State, event: WireEvent) => State;

// Durable rows only match durable identities. Optimistic submission identity is
// owned by localSubmissionState and must never replace a canonical item id.
export function matchingSnapshotItem(items: Item[], item: Item): Item | undefined {
  if (item.kind === "user") {
    const users = items.filter((candidate): candidate is Extract<Item, { kind: "user" }> => candidate.kind === "user");
    const message = item.messageId && users.find(candidate => candidate.messageId === item.messageId);
    if (message) return message;
  }
  return items.find(candidate => candidate.id === item.id);
}

function recordItemOrder(records: TranscriptRecord[], convert: Convert): Record<string, number> {
  const order: Record<string, number> = {};
  for (const record of records) {
    const items = convert([{ ...record.message, recordId: record.id }], "snapshot:").items;
    items.forEach((item, index) => { order[item.id] = Math.min(order[item.id] ?? Infinity, record.order + index / (items.length + 1)); });
  }
  return order;
}

export function transcriptPageState(state: State, page: TranscriptSnapshot, convert: Convert): State {
  const records = snapshotRecords({ ...page, activeRecords: [] });
  const converted = convert(records.map((record) => ({ ...record.message, recordId: record.id })), "snapshot:");
  const existing = new Map(state.items.map((item) => [item.id, item]));
  const order = { ...state.transcriptItemOrder };
  for (const [id, position] of Object.entries(recordItemOrder(records, convert))) order[id] = Math.min(order[id] ?? Infinity, position);
  const prefix: Item[] = [];
  for (const item of converted.items) {
    const prior = existing.get(item.id) ?? matchingSnapshotItem(state.items, item);
    if (!prior) { prefix.push(item); continue; }
    if (prior.kind === "tool" && item.kind === "tool") {
      prefix.push({ ...prior, args: prior.args || item.args, messageId: prior.messageId || item.messageId,
        name: prior.name === "tool" ? item.name : prior.name, subject: prior.subject ?? item.subject,
        summary: prior.summary ?? item.summary, fileDiff: prior.fileDiff ?? item.fileDiff });
    } else prefix.push(prior.id === item.id ? prior : { ...prior, ...item, id: item.id } as Item);
  }
  const prefixIDs = new Set(prefix.map((item) => item.id));
  const added = prefix.filter((item) => !existing.has(item.id)).length;
  const users = records.map((record) => record.message.historyTurn).filter((turn): turn is number => typeof turn === "number" && turn > 0);
  const items = [...prefix, ...state.items.filter((item) => !prefixIDs.has(item.id))];
  items.sort((a, b) => (order[a.id] ?? Infinity) - (order[b.id] ?? Infinity));
  return settleLocalSubmissions({ ...state, items, transcriptItemOrder: order,
    seq: Math.max(state.seq, converted.seq), historyPrefixCount: state.historyPrefixCount + added,
    historyStartTurn: Math.min(state.historyStartTurn, ...users.map((turn) => turn - 1)),
    historyHasOlder: page.hasOlder, historyOlderLoading: false, historyOlderError: undefined,
    historyHasNewer: false, historyNewerLoading: false, historyNewerError: undefined,
    historyMutation: { seq: state.historyMutation.seq + 1, kind: "prepend" } }, items, canonicalUserConfirmations(converted.items));
}

/** One reducer transaction installs rows, runtime and the active attempt.
 * The event projector advances coverage only after this function commits. */
export function transcriptSnapshotState(state: State, snapshot: TranscriptSnapshot, convert: Convert, applyEvent: ApplyEvent, clock: number, projectedItems?: Item[]): State {
  const sessionId = snapshot.identity.sessionId;
  if (state.transcriptSessionId && state.transcriptSessionId !== sessionId) {
    state = { ...state, localSubmissions: {}, localSubmissionOrder: [], visibleSubmissionHandoffs: {},
      pendingSubmissionId: undefined, pendingUser: undefined, sessionGen: state.sessionGen + 1 };
  }
  const records = snapshotRecords(snapshot);
  const messages = records.map((record) => ({ ...record.message, recordId: record.id }));
  const converted = projectedItems ? { items: projectedItems, seq: state.seq } : convert(messages, "snapshot:");
  state = settleLocalSubmissions(state, converted.items, canonicalUserConfirmations(messages.map(message => ({
    kind: message.role, messageId: message.messageId, submissionId: message.submissionId, turnId: message.turnId,
  }))));
  const order = projectedItems ? Object.fromEntries(projectedItems.map((item, index) => [item.id, index])) : recordItemOrder(records, convert);
  const users = state.items.filter((item): item is Extract<Item, { kind: "user" }> => item.kind === "user");
  const items = converted.items.map((item) => {
    if (item.kind === "tool" && item.resultMissing && snapshot.runtime.status &&
      ["queued", "in_progress", "waiting_user", "cancelling"].includes(snapshot.runtime.status) &&
      messages.some(message => message.turnId === snapshot.runtime.turnId && message.toolCalls?.some(call => call.id === item.id))) {
      return { ...item, status: "running" as const };
    }
    if (item.kind !== "user") return item;
    const mounted = matchingSnapshotItem(users, item);
    if (!mounted) return item;
    const next = { ...mounted, ...item, id: item.id };
    return Object.entries(next).every(([key, value]) => (mounted as unknown as Record<string, unknown>)[key] === value) ? mounted : next;
  });
  const hasLocalSubmission = Boolean(state.pendingSubmissionId && state.localSubmissions[state.pendingSubmissionId]
    && state.localSubmissions[state.pendingSubmissionId].status !== "failed");
  const active = snapshot.runtime.status === "queued" || snapshot.runtime.status === "in_progress" ||
    snapshot.runtime.status === "waiting_user" || snapshot.runtime.status === "cancelling";
  let next: State = {
    ...state,
    transcriptSessionId: sessionId,
    transcriptProtocol: 1,
    transcriptItemOrder: order,
    discardTurn: false,
    assistantSegmentOrdinal: active ? 1 : 0,
    turnStartAt: snapshot.runtime.startedAt ?? (state.activeTurnId === snapshot.runtime.turnId ? state.turnStartAt : 0),
    resolvedPromptId: undefined,
    items: [...items, ...users.filter(user => user.failed && !items.some(item => item.id === user.id)),
      ...state.items.filter(item => item.kind === "notice" && item.local)],
    offscreenItems: undefined,
    seq: Math.max(state.seq, converted.seq),
    running: active || hasLocalSubmission,
    turnActive: active,
    pendingPrompt: false,
    cancelRequested: snapshot.runtime.status === "cancelling",
    cancellable: active || hasLocalSubmission,
    activeTurnId: active ? snapshot.runtime.turnId : undefined,
    turnPhase: active ? snapshot.runtime.phase : undefined,
    completionSummary: snapshot.runtime.completionSummary,
    runtimeStatusEpoch: snapshot.identity.runtimeEpoch,
    runtimeStatusSeq: snapshot.coveredThroughSeq,
    runtimeStatusSnapshotAt: clock,
    turnLifecycleObservedAt: clock,
    pendingUser: hasLocalSubmission ? state.pendingUser : undefined,
    pendingSubmissionId: hasLocalSubmission ? state.pendingSubmissionId : undefined,
    live: undefined,
    currentAssistant: undefined,
    streamAttemptJournal: undefined,
    approval: undefined,
    ask: undefined,
    mcpInteraction: undefined,
    retry: undefined,
    promptArrivedAt: undefined,
    promptArrivedId: undefined,
    promptEpoch: state.promptEpoch + 1,
    promptWaitStartedAt: undefined,
    turnWaitAccumMs: 0,
    hydrateHistoryLoaded: true,
    hydratePlaceholderItems: undefined,
    historyPrefixCount: items.length,
    historyStartTurn: Math.max(0, Math.min(...messages.filter((m) => m.role === "user" && m.historyTurn).map((m) => m.historyTurn!), snapshot.totalTurns) - 1),
    historyEndTurn: snapshot.totalTurns,
    historyTotalTurns: snapshot.totalTurns,
    historyHasOlder: snapshot.hasOlder,
    historyHasNewer: false,
    historyOlderLoading: false,
    historyOlderError: undefined,
    historyNewerLoading: false,
    historyNewerError: undefined,
    historyRevision: undefined,
    historyDigest: undefined,
    historyMutation: { seq: state.historyMutation.seq + 1, kind: "replace" },
  };
  for (const event of snapshot.runtime.pendingEvents ?? []) {
    next = applyEvent(next, { ...event, runtimeEpoch: snapshot.identity.runtimeEpoch });
  }
  const attempts = snapshot.activeAttempts ?? [];
  const attempt = attempts[attempts.length - 1];
  if (active && attempt) {
    const id = `m:${attempt.messageId}`;
    const message = messages.find((message) => message.messageId === attempt.messageId);
    const live = { id, text: message?.content ?? "", reasoning: message?.reasoning ?? "", reasoningComplete: false };
    next = { ...next, currentAssistant: id, live, streamAttemptJournal: {
      id: attempt.id,
      baselineLive: { ...live, text: "", reasoning: "" },
      baselineTurnArgChars: 0,
      createdToolIds: message?.toolCalls?.map((tool) => tool.id).filter(Boolean) ?? [],
      priorTools: {},
    } };
  }
  return next;
}
