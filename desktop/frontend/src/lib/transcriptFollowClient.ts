import type { TranscriptFollowResponse, FollowRequest } from "../generated/desktopContract.generated";
import { addBreadcrumb } from "./breadcrumbs";

export type TranscriptConnection = "syncing" | "connected" | "disconnected";
export type TranscriptErrorKind =
  | "transport"
  | "service_unavailable"
  | "protocol_mismatch"
  | "subscription_reset"
  | "revision_gap"
  | "business_gap"
  | "snapshot_stale"
  | "history_corruption"
  | "consumer_install"
  | "cancelled";
type Change = NonNullable<TranscriptFollowResponse["changes"]>[number];

export class TranscriptSyncError extends Error {
  constructor(readonly kind: TranscriptErrorKind, message: string, readonly cause?: unknown) {
    super(message);
    this.name = "TranscriptSyncError";
  }
}

export interface FollowConsumer {
  install(response: TranscriptFollowResponse): Promise<void> | void;
  changes(changes: Change[]): Promise<void> | void;
  connection(state: TranscriptConnection, error?: string): void;
}

interface CandidateState {
  revision: number;
  coverage: number;
  indexes: Map<string, number>;
  attempts: Map<string, string>;
  results: Map<string, number>;
}

interface TranscriptFollowClientOptions {
  random?: () => number;
  wait?: (milliseconds: number) => Promise<void>;
  waitForService?: () => Promise<void>;
}

const TRANSPORT_DELAYS = [500, 1000, 2000, 4000, 8000] as const;

function errorDetails(error: unknown): Record<string, unknown> | undefined {
  if (typeof error !== "object" || error === null) return undefined;
  const details = (error as { details?: unknown }).details;
  return typeof details === "object" && details !== null ? details as Record<string, unknown> : undefined;
}

function classifyError(error: unknown): TranscriptSyncError {
  if (error instanceof TranscriptSyncError) return error;
  const value = error as { name?: unknown; code?: unknown; message?: unknown } | null;
  const message = value && typeof value.message === "string" ? value.message : String(error);
  if (value?.name === "AbortError") return new TranscriptSyncError("cancelled", message, error);
  const details = errorDetails(error);
  const structured = typeof details?.kind === "string" ? details.kind : "";
  if (value?.code === -32002 || structured === "service_unavailable") {
    return new TranscriptSyncError("service_unavailable", message, error);
  }
  const mapped: Partial<Record<string, TranscriptErrorKind>> = {
    transcript_protocol_mismatch: "protocol_mismatch",
    transcript_subscription_reset: "subscription_reset",
    transcript_revision_gap: "revision_gap",
    transcript_business_gap: "business_gap",
    transcript_snapshot_stale: "snapshot_stale",
    transcript_history_corruption: "history_corruption",
    transcript_consumer_install: "consumer_install",
  };
  if (mapped[structured]) return new TranscriptSyncError(mapped[structured]!, message, error);
  // Legacy transports only carried text. They receive the bounded transport
  // budget, never the old unbounded one-second resync loop.
  return new TranscriptSyncError("transport", message, error);
}

/** One ordered consumer for both transports. It owns synchronization only and
 * never submits a model request or replays a completed tool operation. */
export class TranscriptFollowClient {
  private generation = 0;
  private subscription = "";
  private revision = 0;
  private coverage = 0;
  private identity = "";
  private readonly indexes = new Map<string, number>();
  private readonly attemptMessages = new Map<string, string>();
  private readonly results = new Map<string, number>();
  private readonly cancelWaiters = new Set<() => void>();
  private readonly resetKeys = new Set<string>();
  private readonly random: () => number;

  constructor(private readonly read: (request: FollowRequest) => Promise<TranscriptFollowResponse>,
    private readonly options: TranscriptFollowClientOptions = {}) {
    this.random = options.random ?? Math.random;
  }

  async start(consumer: FollowConsumer): Promise<void> {
    this.stop();
    const generation = this.generation;
    this.resetKeys.clear();
    consumer.connection("syncing");
    try {
      await this.synchronize(generation, consumer, true);
    } catch (error) {
      if (generation === this.generation) consumer.connection("disconnected", String(error));
      throw error;
    }
    if (generation === this.generation) void this.follow(generation, consumer);
  }

  stop(): void {
    this.generation++;
    for (const cancel of [...this.cancelWaiters]) cancel();
    this.cancelWaiters.clear();
    const subscription = this.subscription;
    this.subscription = "";
    if (subscription) void this.read({ subscription, close: true }).catch(() => undefined);
  }

  private async baseline(generation: number, consumer: FollowConsumer): Promise<void> {
    const response = await this.read({});
    if (generation !== this.generation) {
      if (response.subscription) void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new TranscriptSyncError("cancelled", "transcript synchronization was cancelled");
    }
    if (response.protocolVersion !== 2 || !response.snapshot || !response.subscription) {
      if (response.subscription) void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new TranscriptSyncError("protocol_mismatch", "Transcript v2 is required. Upgrade Desktop and Serve together.");
    }
    const snapshot = response.snapshot;
    const identity = JSON.stringify(snapshot.identity);
    if (identity === this.identity && snapshot.projectionRevision < this.revision) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new TranscriptSyncError("revision_gap", "transcript snapshot revision regressed");
    }
    if (response.history && response.history.status !== "ready") {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      const kind = response.history.status === "stale" ? "snapshot_stale" : "history_corruption";
      throw new TranscriptSyncError(kind, `transcript history ${response.history.status}`);
    }
    try {
      await consumer.install(response);
    } catch (error) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      const classified = classifyError(error);
      throw classified.kind === "snapshot_stale" || classified.kind === "history_corruption"
        ? classified : new TranscriptSyncError("consumer_install", classified.message, error);
    }
    if (generation !== this.generation) {
      void this.read({ subscription: response.subscription, close: true }).catch(() => undefined);
      throw new TranscriptSyncError("cancelled", "transcript synchronization was cancelled");
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

  private validate(changes: Change[]): { accepted: Change[]; state: CandidateState } {
    let revision = this.revision;
    let coverage = this.coverage;
    const indexes = new Map(this.indexes);
    const attempts = new Map(this.attemptMessages);
    const results = new Map(this.results);
    const accepted: Change[] = [];
    for (const change of [...changes].sort((a, b) => a.revision - b.revision)) {
      if (change.revision <= revision) continue;
      if (change.resetRequired) throw new TranscriptSyncError("subscription_reset", "transcript subscription reset");
      if (change.revision !== revision + 1) throw new TranscriptSyncError("revision_gap", "transcript revision gap");
      if (change.firstSeq) {
        if (change.firstSeq !== coverage + 1 || change.commitSeq < change.firstSeq) throw new TranscriptSyncError("business_gap", "transcript business gap");
        coverage = change.commitSeq;
      } else if (change.commitSeq !== coverage) throw new TranscriptSyncError("business_gap", "transcript frame cut mismatch");
      const event = change.event;
      for (const record of change.records ?? []) if (record.messageId) results.set(record.messageId, change.commitSeq);
      while (results.size > 192) results.delete(results.keys().next().value!);
      if (event?.kind === "stream_attempt" && event.streamAttempt?.action === "begin") {
        if (!event.messageId) throw new TranscriptSyncError("business_gap", "transcript sampling identity missing");
        indexes.set(event.streamAttempt.id, 0);
        attempts.set(event.streamAttempt.id, event.messageId);
      }
      if (change.attemptId && !change.resultSeq && event?.kind !== "stream_attempt") {
        if (indexes.get(change.attemptId) !== change.index) throw new TranscriptSyncError("business_gap", "transcript sampling gap");
        indexes.set(change.attemptId, change.index + 1);
      }
      if (change.resultSeq && (change.resultSeq > coverage || !["message/complete", "message/interrupted"].includes(change.resultKind ?? ""))) {
        throw new TranscriptSyncError("business_gap", "transcript settlement is not committed");
      }
      if (change.resultSeq) {
        const message = attempts.get(change.attemptId ?? "");
        if (!message || message !== event?.messageId || (results.has(message) && results.get(message) !== change.resultSeq)) {
          throw new TranscriptSyncError("business_gap", "transcript settlement identity mismatch");
        }
      }
      if (event?.kind === "stream_attempt" && event.streamAttempt?.action !== "begin") {
        indexes.delete(event.streamAttempt?.id ?? "");
        attempts.delete(event.streamAttempt?.id ?? "");
      }
      revision = change.revision;
      accepted.push(change);
    }
    return { accepted, state: { revision, coverage, indexes, attempts, results } };
  }

  private commit(state: CandidateState): void {
    this.revision = state.revision;
    this.coverage = state.coverage;
    this.indexes.clear(); for (const [id, index] of state.indexes) this.indexes.set(id, index);
    this.attemptMessages.clear(); for (const [id, message] of state.attempts) this.attemptMessages.set(id, message);
    this.results.clear(); for (const [id, sequence] of state.results) this.results.set(id, sequence);
  }

  private recoveryKey(): string {
    return `${this.identity}:${this.revision}:${this.coverage}`;
  }

  private closeSubscription(): void {
    const old = this.subscription;
    this.subscription = "";
    if (old) void this.read({ subscription: old, close: true }).catch(() => undefined);
  }

  private async wait(generation: number, milliseconds: number): Promise<boolean> {
    return new Promise(resolve => {
      if (generation !== this.generation) { resolve(false); return; }
      let settled = false;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const finish = (value: boolean) => {
        if (settled) return;
        settled = true;
        if (timer) clearTimeout(timer);
        this.cancelWaiters.delete(cancel);
        resolve(value && generation === this.generation);
      };
      const cancel = () => finish(false);
      this.cancelWaiters.add(cancel);
      if (this.options.wait) this.options.wait(milliseconds).then(() => finish(true), () => finish(false));
      else timer = setTimeout(() => finish(true), milliseconds);
    });
  }

  private async waitForService(generation: number): Promise<boolean> {
    const pending = this.options.waitForService?.();
    if (!pending) return this.wait(generation, TRANSPORT_DELAYS[0]);
    return new Promise(resolve => {
      if (generation !== this.generation) { resolve(false); return; }
      let settled = false;
      const finish = (value: boolean) => {
        if (settled) return;
        settled = true;
        this.cancelWaiters.delete(cancel);
        resolve(value && generation === this.generation);
      };
      const cancel = () => finish(false);
      this.cancelWaiters.add(cancel);
      pending.then(() => finish(true), () => finish(false));
    });
  }

  private async synchronize(generation: number, consumer: FollowConsumer, initial: boolean): Promise<void> {
    let failures = 0;
    while (generation === this.generation) {
      try {
        await this.baseline(generation, consumer);
        return;
      } catch (raw) {
        const error = classifyError(raw);
        if (error.kind === "cancelled" || generation !== this.generation) return;
        consumer.connection("disconnected", String(error));
        addBreadcrumb("transcript.v2", `sync_failed reason=${error.kind} revision=${this.revision} commit=${this.coverage} attempts=${this.indexes.size}`);
        if (error.kind === "service_unavailable") {
          if (!await this.waitForService(generation)) return;
        } else if (error.kind === "transport" && failures < TRANSPORT_DELAYS.length) {
          const base = TRANSPORT_DELAYS[failures++];
          const delay = Math.round(base * (0.8 + this.random() * 0.4));
          if (!await this.wait(generation, delay)) return;
        } else {
          throw error;
        }
        if (generation === this.generation) consumer.connection("syncing");
      }
    }
    if (initial) throw new TranscriptSyncError("cancelled", "transcript synchronization was cancelled");
  }

  private async follow(generation: number, consumer: FollowConsumer): Promise<void> {
    let transportFailures = 0;
    while (generation === this.generation) {
      try {
        if (!this.subscription) await this.baseline(generation, consumer);
        if (generation !== this.generation) return;
        const response = await this.read({ subscription: this.subscription, afterRevision: this.revision });
        if (generation !== this.generation) return;
        if (response.protocolVersion !== 2) throw new TranscriptSyncError("protocol_mismatch", "Transcript v2 is required. Upgrade Desktop and Serve together.");
        if (response.resetRequired) throw new TranscriptSyncError("subscription_reset", "transcript subscription reset");
        const candidate = this.validate(response.changes ?? []);
        await consumer.changes(candidate.accepted);
        if (generation !== this.generation) return;
        this.commit(candidate.state);
        transportFailures = 0;
        consumer.connection("connected");
      } catch (raw) {
        if (generation !== this.generation) return;
        const error = classifyError(raw);
        this.closeSubscription();
        consumer.connection("disconnected", String(error));
        addBreadcrumb("transcript.v2", `resync reason=${error.kind} revision=${this.revision} commit=${this.coverage} attempts=${this.indexes.size}`);
        if (["subscription_reset", "revision_gap", "business_gap", "snapshot_stale"].includes(error.kind)) {
          const key = this.recoveryKey();
          if (this.resetKeys.has(key)) return;
          this.resetKeys.add(key);
          consumer.connection("syncing");
          try { await this.synchronize(generation, consumer, false); } catch (terminal) {
            if (generation === this.generation) consumer.connection("disconnected", String(terminal));
            return;
          }
          continue;
        }
        if (error.kind === "service_unavailable") {
          if (!await this.waitForService(generation)) return;
          if (generation === this.generation) consumer.connection("syncing");
          continue;
        }
        if (error.kind === "transport" && transportFailures < TRANSPORT_DELAYS.length) {
          const base = TRANSPORT_DELAYS[transportFailures++];
          const delay = Math.round(base * (0.8 + this.random() * 0.4));
          if (!await this.wait(generation, delay)) return;
          if (generation === this.generation) consumer.connection("syncing");
          continue;
        }
        return;
      }
    }
  }
}
