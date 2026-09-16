import { app } from "./bridge";
import { entriesFor, registerTranscriptContentRecovery } from "./canonicalTranscriptBackend";
import { TranscriptFollowClient } from "./transcriptFollowClient";
import { getTranscriptStore } from "./transcriptStore";
import type { Action } from "./useController";
import type { HistoryEntry, HistoryMessage, WireEvent } from "./types";
import type { TranscriptSnapshot } from "./transcriptProtocol";
import type { Message, TranscriptFollowResponse } from "../generated/desktopContract.generated";

export class TranscriptSessionFollower {
  private readonly client: TranscriptFollowClient;
  private readonly orders = new Map<string, number>();
  private nextOrder = 0;
  private turn = 0;
  private generation = 0;
  private releaseContentRecovery?: () => void;
  private recoveringContent = false;
  metrics = { entries: 0, inlineBytes: 0 };

  constructor(private readonly tabId: string, private readonly path: string, private readonly remote: boolean,
    private readonly dispatch: (action: Action) => void) {
    this.client = new TranscriptFollowClient(request => {
      const read = remote ? app.RemoteTranscriptFollowForTab : app.TranscriptFollowForTab;
      if (!read) return Promise.reject(new Error("Transcript v2 is required. Upgrade Desktop and Serve together."));
      return read(tabId, request);
    });
  }

  async start(): Promise<void> {
    this.generation++;
    this.releaseContentRecovery?.();
    this.releaseContentRecovery = registerTranscriptContentRecovery(this.tabId, () => {
      if (this.recoveringContent) return;
      this.recoveringContent = true;
      void this.start().catch(() => undefined).finally(() => { this.recoveringContent = false; });
    });
    await this.client.start({
      install: response => this.install(response),
      changes: changes => {
        for (const change of changes) {
          if (change.records?.length) {
            const entries = change.records.map(message => this.entry(message));
            const projection = getTranscriptStore().upsertEntries(this.tabId, this.path, entries, change.commitSeq);
            if (projection) this.dispatch({ type: "transcript_records", projection });
          }
          if (change.event) this.dispatch({ type: "event", e: { ...change.event, tabId: this.tabId } as unknown as WireEvent, remote: this.remote });
          if (change.runtime) this.dispatch({ type: "transcript_runtime", runtime: change.runtime });
        }
      },
      connection: (status, error) => this.dispatch({ type: "transcript_connection", status, error }),
    });
  }

  stop(): void { this.generation++; this.releaseContentRecovery?.(); this.releaseContentRecovery = undefined; this.client.stop(); }

  private entry(message: Message): HistoryEntry {
    const entryId = message.messageId ? `m:${message.messageId}` : message.recordId!;
    let order = this.orders.get(entryId);
    if (order === undefined) {
      order = this.nextOrder++; this.orders.set(entryId, order);
      if (message.role === "user") this.turn++;
      // Only the resident tail needs an order index. Older pages carry their
      // canonical positions and are owned by the bounded transcript store.
      while (this.orders.size > 192) this.orders.delete(this.orders.keys().next().value!);
    }
    return { entryId, order, turn: message.historyTurn || this.turn, message: message as unknown as HistoryMessage, refs: [] };
  }

  private async install(response: TranscriptFollowResponse): Promise<void> {
    const generation = this.generation;
    const snapshot = response.snapshot!;
    // A suffix must never be applied to a truncated prefix. Resolve active
    // snapshot references before publishing any part of this recovery cut.
    const activeIds = new Set(snapshot.activeAttempts.map(attempt => attempt.messageId));
    for (const record of [...snapshot.records, ...snapshot.activeRecords]) {
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
    const page = response.history;
    this.turn = page?.totalTurns ?? snapshot.totalTurns;
    this.orders.clear();
    const entries = entriesFor(page?.messages ?? [], page?.snapshotSequence ?? snapshot.coveredThroughSeq);
    for (const entry of entries) this.orders.set(entry.entryId, entry.order);
    this.nextOrder = Math.max(0, ...entries.map(entry => entry.order + 1));
    const merged = new Map(entries.map(entry => [entry.entryId, entry]));
    for (const record of [...snapshot.records, ...snapshot.activeRecords]) {
      const entry = this.entry(record.message);
      // Durable canonical refs remain loadable after a view snapshot expires.
      const canonical = merged.get(entry.entryId);
      if (canonical && !snapshot.activeAttempts.some(attempt => attempt.messageId === record.message.messageId)) continue;
      entry.refs = record.refs.map(ref => ({ entryId: entry.entryId, field: ref.path[0], size: ref.bytes, chunks: 1,
        revision: snapshot.coveredThroughSeq, revKnown: true, digest: snapshot.snapshotId, transcriptRef: ref }));
      merged.set(entry.entryId, entry);
    }
    const all = [...merged.values()].sort((a, b) => a.order - b.order);
    this.metrics = { entries: all.length, inlineBytes: all.reduce((bytes, entry) => bytes + entry.message.content.length + (entry.message.reasoning?.length ?? 0), 0) };
    const projection = getTranscriptStore().installSlice(this.tabId, this.path, {
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
    this.dispatch({ type: "transcript_v2_snapshot", snapshot: combined, projection, remote: this.remote });
    this.dispatch({ type: "transcript_runtime", runtime: snapshot.runtime });
  }
}
