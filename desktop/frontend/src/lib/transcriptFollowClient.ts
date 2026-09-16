import type { TranscriptFollowResponse, FollowRequest } from "../generated/desktopContract.generated";
import { addBreadcrumb } from "./breadcrumbs";

export type TranscriptConnection = "syncing" | "connected" | "disconnected";
type Change = NonNullable<TranscriptFollowResponse["changes"]>[number];

export interface FollowConsumer {
  install(response: TranscriptFollowResponse): Promise<void> | void;
  changes(changes: Change[]): void;
  connection(state: TranscriptConnection, error?: string): void;
}

/** One ordered consumer for both transports. Connection failures only request
 * another snapshot; this class has no model, submit, stop or retry-turn API. */
export class TranscriptFollowClient {
  private generation = 0;
  private subscription = "";
  private revision = 0;
  private coverage = 0;
  private identity = "";
  private readonly indexes = new Map<string, number>();
  private readonly attemptMessages = new Map<string, string>();
  private readonly results = new Map<string, number>();

  constructor(private readonly read: (request: FollowRequest) => Promise<TranscriptFollowResponse>) {}

  async start(consumer: FollowConsumer): Promise<void> {
    this.stop();
    const generation = this.generation;
    consumer.connection("syncing");
    try { await this.baseline(generation, consumer); }
    catch (error) {
      if (generation === this.generation) consumer.connection("disconnected", String(error));
      throw error;
    }
    if (generation === this.generation) void this.follow(generation, consumer);
  }

  stop(): void {
    this.generation++;
    const subscription = this.subscription;
    this.subscription = "";
    if (subscription) void this.read({ subscription, close: true }).catch(() => undefined);
  }

  private async baseline(generation: number, consumer: FollowConsumer): Promise<void> {
    const response = await this.read({});
    if (generation !== this.generation) {
      if (response.subscription) void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      return;
    }
    if (response.protocolVersion !== 2 || !response.snapshot || !response.subscription) {
      if (response.subscription) void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new Error("Transcript v2 is required. Upgrade Desktop and Serve together.");
    }
    const snapshot = response.snapshot;
    const identity = JSON.stringify(snapshot.identity);
    if (identity === this.identity && snapshot.projectionRevision < this.revision) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new Error("transcript snapshot revision regressed");
    }
    if (response.history && response.history.status !== "ready") {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new Error(`transcript history ${response.history.status}`);
    }
    try { await consumer.install(response); } catch (error) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw error;
    }
    if (generation !== this.generation) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      return;
    }
    this.identity = identity;
    this.subscription = response.subscription;
    this.revision = snapshot.projectionRevision;
    this.coverage = snapshot.coveredThroughSeq;
    this.indexes.clear();
    this.attemptMessages.clear();
    this.results.clear();
    for (const attempt of snapshot.activeAttempts ?? []) {
      this.indexes.set(attempt.id, attempt.nextIndex ?? 0);
      this.attemptMessages.set(attempt.id, attempt.messageId);
    }
    addBreadcrumb("transcript.v2", `snapshot epoch=${snapshot.identity.runtimeEpoch} revision=${this.revision} commit=${this.coverage} durable=${snapshot.durableSeq} records=${snapshot.totalRecords} attempts=${this.indexes.size}`);
    consumer.connection("connected");
  }

  private validate(changes: Change[]): Change[] {
    let revision = this.revision;
    let coverage = this.coverage;
    const indexes = new Map(this.indexes);
    const attempts = new Map(this.attemptMessages);
    const results = new Map(this.results);
    const accepted: Change[] = [];
    // Transport coalescing can reorder frames within a delivered batch. Their
    // publisher revisions establish order; an actual missing revision resets.
    for (const change of [...changes].sort((a, b) => a.revision - b.revision)) {
      if (change.revision <= revision) continue;
      if (change.resetRequired || change.revision !== revision + 1) throw new Error("transcript revision gap");
      if (change.firstSeq) {
        if (change.firstSeq !== coverage + 1 || change.commitSeq < change.firstSeq) throw new Error("transcript business gap");
        coverage = change.commitSeq;
      } else if (change.commitSeq !== coverage) throw new Error("transcript frame cut mismatch");
      const event = change.event;
      for (const record of change.records ?? []) if (record.messageId) results.set(record.messageId, change.commitSeq);
      while (results.size > 192) results.delete(results.keys().next().value!);
      if (event?.kind === "stream_attempt" && event.streamAttempt?.action === "begin") {
        if (!event.messageId) throw new Error("transcript sampling identity missing");
        indexes.set(event.streamAttempt.id, 0);
        attempts.set(event.streamAttempt.id, event.messageId);
      }
      if (change.attemptId && !change.resultSeq && event?.kind !== "stream_attempt") {
        if (indexes.get(change.attemptId) !== change.index) throw new Error("transcript sampling gap");
        indexes.set(change.attemptId, change.index + 1);
      }
      if (change.resultSeq && (change.resultSeq > coverage || !["message/complete", "message/interrupted"].includes(change.resultKind ?? ""))) throw new Error("transcript settlement is not committed");
      if (change.resultSeq) {
        const message = attempts.get(change.attemptId ?? "");
        if (!message || message !== event?.messageId || (results.has(message) && results.get(message) !== change.resultSeq)) throw new Error("transcript settlement identity mismatch");
      }
      if (event?.kind === "stream_attempt" && event.streamAttempt?.action !== "begin") {
        indexes.delete(event.streamAttempt?.id ?? ""); attempts.delete(event.streamAttempt?.id ?? "");
      }
      revision = change.revision;
      accepted.push(change);
    }
    this.revision = revision;
    this.coverage = coverage;
    this.indexes.clear();
    for (const [id, index] of indexes) this.indexes.set(id, index);
    this.attemptMessages.clear(); for (const [id, message] of attempts) this.attemptMessages.set(id, message);
    this.results.clear(); for (const [id, sequence] of results) this.results.set(id, sequence);
    return accepted;
  }

  private async follow(generation: number, consumer: FollowConsumer): Promise<void> {
    while (generation === this.generation) {
      try {
        if (!this.subscription) await this.baseline(generation, consumer);
        if (generation !== this.generation) return;
        const response = await this.read({ subscription: this.subscription, afterRevision: this.revision });
        if (generation !== this.generation) return;
        if (response.protocolVersion !== 2 || response.resetRequired) throw new Error("transcript requires resynchronization");
        consumer.changes(this.validate(response.changes ?? []));
        consumer.connection("connected");
      } catch (error) {
        if (generation !== this.generation) return;
        const old = this.subscription;
        this.subscription = "";
        if (old) void this.read({ subscription: old, close: true }).catch(() => undefined);
        consumer.connection("disconnected", String(error));
        addBreadcrumb("transcript.v2", `resync reason=transport_or_protocol revision=${this.revision} commit=${this.coverage} attempts=${this.indexes.size}`);
        await new Promise(resolve => setTimeout(resolve, 1000));
        if (generation === this.generation) consumer.connection("syncing");
      }
    }
  }
}
