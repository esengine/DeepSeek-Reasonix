import { noteSessionObservation } from "./sessionObservationDiagnostics";
import { app } from "./bridge";
import { entriesFor, registerTranscriptContentRecovery } from "./canonicalTranscriptBackend";
import { TranscriptFollowClient } from "./transcriptFollowClient";
import { getTranscriptStore } from "./transcriptStore";
import type { Action, State } from "./useController";
import type { HistoryEntry, HistoryMessage, WireEvent } from "./types";
import type { TranscriptSnapshot } from "./transcriptProtocol";
import type { Message, TranscriptFollowResponse } from "../generated/desktopContract.generated";
import { canonicalUserConfirmations } from "./localSubmissionState";
import { snapshotRecords } from "./transcriptSnapshotState";

export class TranscriptSessionFollowerRuntime {
  private readonly client: TranscriptFollowClient;
  private readonly orders = new Map<string, number>();
  private nextOrder = 0;
  private turn = 0;
  private generation = 0;
  private releaseContentRecovery?: () => void;
  private recoveringContent = false;
  private coverage = 0;
  private recoveryScheduled = false;
  private readonly confirmationReads = new Map<string, { coverage: number; pending: boolean }>();
  private readonly submissionCoverage = new Map<string, number>();
  metrics = { entries: 0, inlineBytes: 0 };

  constructor(private readonly tabId: string, private readonly path: string, private readonly remote: boolean,
    private readonly dispatch: (action: Action) => void,
    private readonly state: () => State | undefined = () => getTranscriptStore().states.get(tabId)) {
    this.client = new TranscriptFollowClient(request => {
      const read = remote ? app.RemoteTranscriptFollowForTab : app.TranscriptFollowForTab;
      if (!read) return Promise.reject(new Error("Transcript v2 is required. Upgrade Desktop and Serve together."));
      return read(tabId, request);
    });
  }

  async start(): Promise<void> {
    this.generation++;
    noteSessionObservation(this.path, { action: "subscribe", tabId: this.tabId, generation: this.generation, sequence: this.coverage });
    this.recoveryScheduled = false;
    this.coverage = 0;
    this.confirmationReads.clear();
    this.submissionCoverage.clear();
    this.observeSubmissions();
    this.releaseContentRecovery?.();
    this.releaseContentRecovery = registerTranscriptContentRecovery(this.tabId, () => {
      if (this.recoveringContent) return;
      this.recoveringContent = true;
      void this.start().catch(() => undefined).finally(() => { this.recoveringContent = false; });
    });
    await this.client.start({
      install: response => this.install(response),
      changes: changes => {
        this.observeSubmissions();
        for (const change of changes) {
          if (change.records?.length) {
            const entries = change.records.map(message => this.entry(message));
            const projection = getTranscriptStore().upsertEntries(this.tabId, this.path, entries, change.commitSeq);
            if (!projection) throw new Error("Transcript window is unavailable; synchronize again");
            this.dispatch({ type: "transcript_records", projection,
              confirmedUsers: canonicalUserConfirmations(entries.map(entry => ({ ...entry.message, kind: entry.message.role }))) });
            this.coverage = Math.max(this.coverage, change.commitSeq);
          }
          if (change.event) this.dispatch({ type: "event", e: { ...change.event, tabId: this.tabId } as unknown as WireEvent, remote: this.remote });
          if (change.runtime) this.dispatch({ type: "transcript_runtime", runtime: change.runtime });
        }
        this.scheduleConfirmationRecovery();
      },
      connection: (status, error) => { noteSessionObservation(this.path, { action: "connection", tabId: this.tabId, generation: this.generation, sequence: this.coverage, status }); this.dispatch({ type: "transcript_connection", status, error }); },
    });
  }

  stop(): void { noteSessionObservation(this.path, { action: "unsubscribe", tabId: this.tabId, generation: this.generation, sequence: this.coverage }); this.generation++; this.confirmationReads.clear(); this.submissionCoverage.clear(); this.releaseContentRecovery?.(); this.releaseContentRecovery = undefined; this.client.stop(); }

  private observeSubmissions(): void {
    const pending = new Set(this.state()?.localSubmissionOrder ?? []);
    for (const id of this.submissionCoverage.keys()) if (!pending.has(id)) this.submissionCoverage.delete(id);
    for (const id of pending) if (!this.submissionCoverage.has(id)) this.submissionCoverage.set(id, this.coverage);
  }

  private scheduleConfirmationRecovery(): void {
    if (this.recoveryScheduled) return;
    this.recoveryScheduled = true;
    const generation = this.generation;
    queueMicrotask(() => {
      this.recoveryScheduled = false;
      if (generation !== this.generation) return;
      const state = this.state();
      if (!state) return;
      this.observeSubmissions();
      const unresolved = new Set(state.localSubmissionOrder);
      for (const key of this.confirmationReads.keys()) if (!unresolved.has(key)) this.confirmationReads.delete(key);
      for (const local of Object.values(state.localSubmissions)) {
        // Identity events alone do not justify a history read. Only recover
        // after a committed cut could have hidden an earlier formal record.
        if (!local.messageId || this.coverage <= (this.submissionCoverage.get(local.submissionId) ?? this.coverage)) continue;
        const previous = this.confirmationReads.get(local.submissionId);
        if (previous?.pending || previous?.coverage === this.coverage) continue;
        const read = this.remote ? app.RemoteSessionHistoryWindowForTab : app.SessionHistoryWindowForTab;
        if (!read) continue;
        const ticket = { coverage: this.coverage, pending: true };
        this.confirmationReads.set(local.submissionId, ticket);
        const current = () => generation === this.generation && this.state()?.sessionGen === state.sessionGen
          && this.state()?.localSubmissions[local.submissionId]?.messageId === local.messageId;
        void read(this.tabId, { anchor: "message", messageId: local.messageId, limit: 1 }).then(page => {
          if (!current() || page.status !== "ready") return;
          const message = page.messages.find(message => message.messageId === local.messageId && message.role === "user");
          if (message) this.dispatch({ type: "submission_verified", submissionId: local.submissionId, messageId: local.messageId! });
        }).catch(() => { /* An inconclusive read retains the echo until the next committed cut. */ }).finally(() => {
          if (this.confirmationReads.get(local.submissionId) !== ticket) return;
          ticket.pending = false;
          if (!current()) this.confirmationReads.delete(local.submissionId);
          else if (ticket.coverage !== this.coverage) this.scheduleConfirmationRecovery();
        });
      }
    });
  }

  private entry(message: Message | HistoryMessage, outerRecordId?: string): HistoryEntry {
    const derived = message.role === "tool" && message.toolCallId ? `tool:${message.toolCallId}`
      : message.messageId ? `m:${message.messageId}` : undefined;
    if (message.recordId && derived && message.recordId !== derived) {
      throw new Error("transcript message identity mismatch");
    }
    if (outerRecordId && message.recordId && outerRecordId !== message.recordId) {
      throw new Error("transcript snapshot record identity mismatch");
    }
    if (outerRecordId && derived && outerRecordId !== derived) {
      throw new Error("transcript snapshot message identity mismatch");
    }
    const entryId = derived ?? message.recordId ?? outerRecordId;
    if (!entryId) throw new Error("invalid transcript snapshot record identity");
    const normalized = message.recordId === entryId ? message : { ...message, recordId: entryId };
    let order = this.orders.get(entryId);
    if (order === undefined) {
      order = this.nextOrder++; this.orders.set(entryId, order);
      if (message.role === "user") this.turn++;
      // Only the resident tail needs an order index. Older pages carry their
      // canonical positions and are owned by the bounded transcript store.
      while (this.orders.size > 192) this.orders.delete(this.orders.keys().next().value!);
    }
    return { entryId, order, turn: message.historyTurn || this.turn, message: normalized as unknown as HistoryMessage, refs: [] };
  }

  private async install(response: TranscriptFollowResponse): Promise<void> {
    const generation = this.generation;
    const snapshot = response.snapshot!;
    // Validate the untrusted bridge cut before resolving refs, merging rows or
    // changing the resident store. The outer record id is authoritative for
    // older peers that did not repeat it inside the message.
    const records = snapshotRecords(snapshot as unknown as TranscriptSnapshot);
    // A suffix must never be applied to a truncated prefix. Resolve active
    // snapshot references before publishing any part of this recovery cut.
    const activeIds = new Set(snapshot.activeAttempts.map(attempt => attempt.messageId));
    for (const record of records) {
      if (!record.message.messageId || !activeIds.has(record.message.messageId)) continue;
      for (const ref of record.refs) {
        const read = this.remote ? app.RemoteTranscriptContentForTab : app.TranscriptContentForTab;
        if (!read) throw new Error("Transcript v2 content is unavailable");
        let offset = 0, text = "";
        while (true) {
          const chunk = await read(this.tabId, { ...ref, offset });
          if (generation !== this.generation) return;
          if (chunk.stale) throw new Error("Active transcript snapshot expired; synchronize again");
          text += chunk.data;
          if (chunk.done) break;
          if (chunk.nextOffset <= offset) throw new Error("Active transcript content did not advance");
          offset = chunk.nextOffset;
        }
        let target = record.message as unknown as Record<string, unknown>;
        for (const key of ref.path.slice(0, -1)) target = target[key] as Record<string, unknown>;
        target[ref.path[ref.path.length - 1]] = text;
      }
      record.refs = [];
    }
    if (generation !== this.generation) return;
    this.observeSubmissions();
    const page = response.history;
    this.turn = page?.totalTurns ?? snapshot.totalTurns;
    this.orders.clear();
    const entries = entriesFor(page?.messages ?? [], page?.snapshotSequence ?? snapshot.coveredThroughSeq);
    for (const entry of entries) this.orders.set(entry.entryId, entry.order);
    this.nextOrder = Math.max(0, ...entries.map(entry => entry.order + 1));
    const merged = new Map(entries.map(entry => [entry.entryId, entry]));
    for (const record of records) {
      const entry = this.entry(record.message, record.id);
      // Durable canonical refs remain loadable after a view snapshot expires.
      const canonical = merged.get(entry.entryId);
      if (canonical && !snapshot.activeAttempts.some(attempt => attempt.messageId === record.message.messageId)) continue;
      entry.refs = record.refs.map(ref => ({ entryId: entry.entryId, field: ref.path[0], size: ref.bytes, chunks: 1,
        revision: snapshot.coveredThroughSeq, revKnown: true, digest: snapshot.snapshotId, transcriptRef: ref }));
      merged.set(entry.entryId, entry);
    }
    const all = [...merged.values()].sort((a, b) => a.order - b.order);
    this.metrics = { entries: all.length, inlineBytes: all.reduce((bytes, entry) => bytes + entry.message.content.length + (entry.message.reasoning?.length ?? 0), 0) };
    const prepared = getTranscriptStore().prepareInstallSlice(this.tabId, this.path, {
      entries: all, nextCursor: page?.olderCursor ?? "", newerCursor: page?.newerCursor ?? "",
      hasOlder: Boolean(page?.hasOlder), hasNewer: false, totalTurns: this.turn,
      startTurn: Math.min(this.turn, ...all.map(entry => entry.turn)), endTurn: this.turn,
      revision: page?.snapshotSequence ?? snapshot.coveredThroughSeq, revisionKnown: true, digest: page?.generation ?? "", stale: false,
    });
    const combined: TranscriptSnapshot = {
      ...snapshot as unknown as TranscriptSnapshot,
      records: all.map((entry, order) => ({ id: entry.entryId, order, message: entry.message, refs: [] })),
      activeRecords: [], totalRecords: all.length, totalTurns: this.turn,
    };
    this.dispatch({ type: "transcript_v2_snapshot", snapshot: combined, projection: prepared.projection, remote: this.remote });
    prepared.commit();
    this.dispatch({ type: "transcript_runtime", runtime: snapshot.runtime });
    this.coverage = snapshot.coveredThroughSeq;
    noteSessionObservation(this.path, { action: "snapshot_installed", tabId: this.tabId, generation: this.generation, sequence: this.coverage, status: snapshot.runtime.status });
    this.scheduleConfirmationRecovery();
  }
}
