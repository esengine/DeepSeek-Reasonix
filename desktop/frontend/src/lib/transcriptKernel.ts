export type ViewportIntent = "tail" | "reader";

export type LogicalAnchor =
  | { kind: "tail" }
  | { kind: "block"; blockKey: string; offsetPx: number };

export type ScrollTransactionKind =
  | "jump"
  | "restore"
  | "prepend"
  | "display-change"
  | "selection"
  | "composer-resize"
  | "tail-sync";

export type ScrollTransaction = {
  id: number;
  generation: number;
  geometryRevision: number;
  kind: ScrollTransactionKind;
  status: "active" | "committed" | "cancelled" | "expired";
};

export type TranscriptScrollOwner =
  | "tail-follow"
  | "question-jump"
  | "restore"
  | "history-prepend"
  | "display-change"
  | "selection-edge-scroll"
  | "composer-resize"
  | "custom-scrollbar"
  | "nested-scroll"
  | "block-window-prepend";

export type TranscriptScrollMode = "tail-follow" | "manual" | "selection" | "restoring";

export type TranscriptViewportGeometry = {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
};

export type TranscriptVisibleBlock = {
  key: string;
  top: number;
  bottom: number;
};

export type TranscriptViewportSnapshot = TranscriptViewportGeometry & {
  visibleBlocks: readonly TranscriptVisibleBlock[];
};

export type TranscriptWriteRequest = {
  session: string;
  generation: number;
  transactionId: number;
  geometryRevision: number;
  owner: TranscriptScrollOwner;
  intent: ViewportIntent;
  offset: number;
};

export type TranscriptWriteResult = {
  accepted: boolean;
  offset: number;
  reason?: string;
  changed?: boolean;
};

export type TranscriptKernelClock = {
  now: () => number;
  requestAnimationFrame: (callback: FrameRequestCallback) => number;
  cancelAnimationFrame: (handle: number) => void;
  setTimeout: (callback: () => void, delay: number) => ReturnType<typeof setTimeout>;
  clearTimeout: (handle: ReturnType<typeof setTimeout>) => void;
};

export type TranscriptKernelEvent = {
  session: string;
  generation: number;
  transaction: number;
  owner?: TranscriptScrollOwner;
  intent: ViewportIntent;
  geometryRevision: number;
  requestedOffset?: number;
  acceptedOffset?: number;
  outcome: string;
};

type ActiveTransaction = {
  listeners?: Set<() => void>;
  transaction: ScrollTransaction;
  anchor: LogicalAnchor;
  correctionRevision: number;
  retryUsed: boolean;
  timeout: ReturnType<typeof setTimeout>;
};

type DeferredStructuralTransaction = {
  generation: number;
  kind: "restore" | "prepend" | "display-change" | "composer-resize";
  anchor: LogicalAnchor;
};

const TRANSACTION_TTL_MS = 1_000;
const BOTTOM_THRESHOLD_PX = 4;

function defaultClock(): TranscriptKernelClock {
  return {
    now: () => Date.now(),
    requestAnimationFrame: (callback) => requestAnimationFrame(callback),
    cancelAnimationFrame: (handle) => cancelAnimationFrame(handle),
    setTimeout: (callback, delay) => setTimeout(callback, delay),
    clearTimeout: (handle) => clearTimeout(handle),
  };
}

function transactionPriority(kind: ScrollTransactionKind): number {
  switch (kind) {
    case "selection": return 5;
    case "jump": return 4;
    case "restore":
    case "prepend":
    case "display-change":
    case "composer-resize": return 3;
    case "tail-sync": return 1;
  }
}

export class TranscriptKernel {
  private readonly clock: TranscriptKernelClock;
  private readonly emit: (event: TranscriptKernelEvent) => void;
  private write: ((request: TranscriptWriteRequest) => TranscriptWriteResult) | null = null;
  private session = "";
  private generationValue = 0;
  private geometryVersion = 0;
  private transactionSequence = 0;
  private interactionVersion = 0;
  private active: ActiveTransaction | null = null;
  private deferredStructural: DeferredStructuralTransaction | null = null;
  private intentValue: ViewportIntent = "tail";
  private anchorValue: LogicalAnchor = { kind: "tail" };
  private anchors = new Map<string, LogicalAnchor>();
  private userGesture = false;
  private tailFrame: number | null = null;
  private anomalyCount = 0;
  private safeModeValue = false;
  private writeTop: number | null = null;
  private nativeGestureTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: { clock?: TranscriptKernelClock; emit?: (event: TranscriptKernelEvent) => void } = {}) {
    this.clock = options.clock ?? defaultClock();
    this.emit = options.emit ?? (() => {});
  }

  get generation(): number { return this.generationValue; }
  get interactionRevision(): number { return this.interactionVersion; }
  get geometryRevision(): number { return this.geometryVersion; }
  get intent(): ViewportIntent { return this.intentValue; }
  get anchor(): LogicalAnchor { return this.anchorValue; }
  get safeMode(): boolean { return this.safeModeValue; }
  get userGestureActive(): boolean { return this.userGesture; }
  get nativeGestureLeaseActive(): boolean { return this.nativeGestureTimer !== null; }
  get activeTransaction(): ScrollTransaction | null { return this.active?.transaction ?? null; }

  connectWriter(writer: (request: TranscriptWriteRequest) => TranscriptWriteResult): () => void {
    this.write = writer;
    return () => {
      if (this.write === writer) this.write = null;
    };
  }

  detachSurface(): void {
    this.clearNativeGestureLease();
    this.cancelActive("surface-detached");
    this.deferredStructural = null;
    if (this.tailFrame !== null) this.clock.cancelAnimationFrame(this.tailFrame);
    this.tailFrame = null;
    this.generationValue += 1;
    this.userGesture = false;
    this.writeTop = null;
  }

  replaceSurface(session: string): { generation: number; anchor: LogicalAnchor } {
    if (this.session) this.anchors.set(this.session, this.anchorValue);
    this.clearNativeGestureLease();
    this.cancelActive("surface-replaced");
    this.deferredStructural = null;
    if (this.tailFrame !== null) this.clock.cancelAnimationFrame(this.tailFrame);
    this.tailFrame = null;
    this.session = session;
    this.generationValue += 1;
    this.geometryVersion = 0;
    this.userGesture = false;
    this.writeTop = null;
    this.anomalyCount = 0;
    this.safeModeValue = false;
    this.anchorValue = this.anchors.get(session) ?? { kind: "tail" };
    this.intentValue = this.anchorValue.kind === "tail" ? "tail" : "reader";
    return { generation: this.generationValue, anchor: this.anchorValue };
  }

  advanceGeometry(generation = this.generationValue): number {
    if (generation !== this.generationValue) return this.geometryVersion;
    this.geometryVersion += 1;
    return this.geometryVersion;
  }

  capture(snapshot: TranscriptViewportSnapshot): LogicalAnchor {
    if (this.intentValue === "tail") return { kind: "tail" };
    const first = snapshot.visibleBlocks.find((block) => block.bottom > snapshot.scrollTop + 0.5);
    if (!first) return this.anchorValue;
    return { kind: "block", blockKey: first.key, offsetPx: snapshot.scrollTop - first.top };
  }

  observeNativeScroll(
    snapshot: TranscriptViewportSnapshot,
    nativeEvent = true,
  ): boolean {
    if (nativeEvent) {
      const writerTop = this.writeTop;
      this.writeTop = null;
      if (writerTop !== null && Math.abs(snapshot.scrollTop - writerTop) <= BOTTOM_THRESHOLD_PX) return false;
    }
    if (nativeEvent && !this.userGesture && this.active) return false;
    const atBottom = snapshot.scrollHeight - snapshot.clientHeight - snapshot.scrollTop <= BOTTOM_THRESHOLD_PX;
    this.intentValue = atBottom ? "tail" : "reader";
    this.anchorValue = this.intentValue === "tail" ? { kind: "tail" } : this.capture(snapshot);
    this.anchors.set(this.session, this.anchorValue);
    return nativeEvent;
  }

  beginUserGesture(snapshot: TranscriptViewportSnapshot, owner: "selection" | "native" = "native"): void {
    this.interactionVersion += 1;
    this.clearNativeGestureLease();
    this.cancelTailFrame();
    this.userGesture = true;
    this.intentValue = "reader";
    this.anchorValue = this.capture(snapshot);
    this.anchors.set(this.session, this.anchorValue);
    if (owner === "selection") this.begin("selection", this.anchorValue);
    else this.cancelActive("user-gesture");
  }

  endUserGesture(): ScrollTransaction | null {
    this.clearNativeGestureLease();
    this.userGesture = false;
    if (this.active?.transaction.kind === "selection") this.finish(this.active.transaction.id, "committed", "selection-ended");
    const deferred = this.deferredStructural;
    this.deferredStructural = null;
    if (!deferred || deferred.generation !== this.generationValue) return null;
    return this.begin(deferred.kind, deferred.anchor);
  }

  renewNativeGesture(
    snapshot: TranscriptViewportSnapshot,
    idleMs: number,
    onEnd: (resumed: ScrollTransaction | null) => void,
  ): void {
    this.clearNativeGestureLease();
    if (this.userGesture) this.observeNativeScroll(snapshot);
    else this.beginUserGesture(snapshot, "native");
    const generation = this.generationValue;
    const timer = this.clock.setTimeout(() => {
      if (generation !== this.generationValue || this.nativeGestureTimer !== timer) return;
      this.nativeGestureTimer = null;
      onEnd(this.endUserGesture());
    }, Math.max(0, idleMs));
    this.nativeGestureTimer = timer;
  }

  afterCurrentGenerationPaint(callback: () => void): () => void {
    const generation = this.generationValue;
    let cancelled = false;
    const handle = this.clock.requestAnimationFrame(() => {
      if (!cancelled && generation === this.generationValue) callback();
    });
    return () => { cancelled = true; this.clock.cancelAnimationFrame(handle); };
  }

  begin(kind: ScrollTransactionKind, anchor = this.anchorValue): ScrollTransaction | null {
    if (this.userGesture && kind !== "selection") {
      if (kind === "restore" || kind === "prepend" || kind === "display-change" || kind === "composer-resize") {
        this.deferredStructural = { generation: this.generationValue, kind, anchor };
      }
      return null;
    }
    if (this.active && transactionPriority(this.active.transaction.kind) > transactionPriority(kind)) return null;
    if (kind !== "tail-sync") this.cancelTailFrame();
    this.cancelActive("superseded");
    const transaction: ScrollTransaction = {
      id: ++this.transactionSequence,
      generation: this.generationValue,
      geometryRevision: this.geometryVersion,
      kind,
      status: "active",
    };
    const timeout = this.clock.setTimeout(() => this.finish(transaction.id, "expired", "deadline"), TRANSACTION_TTL_MS);
    this.active = { transaction, anchor, correctionRevision: -1, retryUsed: false, timeout };
    this.anchorValue = anchor;
    this.intentValue = anchor.kind === "tail" ? "tail" : "reader";
    this.anchors.set(this.session, anchor);
    this.emitEvent(transaction, undefined, undefined, undefined, "active");
    return transaction;
  }

  cancelActive(outcome = "cancelled"): void {
    if (this.active) this.finish(this.active.transaction.id, "cancelled", outcome);
  }

  onTransactionEnd(transaction: ScrollTransaction, listener: () => void): void {
    if (this.active?.transaction !== transaction) { listener(); return; }
    (this.active.listeners ??= new Set()).add(listener);
  }

  finish(id: number, status: Exclude<ScrollTransaction["status"], "active">, outcome: string = status): boolean {
    const active = this.active;
    if (!active || active.transaction.id !== id) return false;
    this.clock.clearTimeout(active.timeout);
    active.transaction.status = status;
    this.emitEvent(active.transaction, undefined, undefined, undefined, outcome);
    this.active = null;
    active.listeners?.forEach((listener) => listener());
    return true;
  }

  correctAnchor(transaction: ScrollTransaction, blockTop: (blockKey: string) => number | undefined): boolean {
    const active = this.active;
    if (!active || active.transaction.id !== transaction.id || transaction.generation !== this.generationValue) return false;
    if (this.userGesture || active.transaction.kind === "selection") return false;
    if (active.correctionRevision === this.geometryVersion) return false;
    active.correctionRevision = this.geometryVersion;
    if (active.anchor.kind === "tail") return this.writeAndFinish(active, "tail-follow", Number.POSITIVE_INFINITY);
    const top = blockTop(active.anchor.blockKey);
    if (top == null || !Number.isFinite(top)) {
      if (!active.retryUsed) {
        active.retryUsed = true;
        return false;
      }
      this.reportAnomaly("missing-anchor");
      this.finish(transaction.id, "cancelled", "anchor-missing");
      return false;
    }
    const owner: TranscriptScrollOwner = active.transaction.kind === "jump" ? "question-jump"
      : active.transaction.kind === "prepend" ? "history-prepend"
        : active.transaction.kind === "display-change" ? "display-change"
          : active.transaction.kind === "composer-resize" ? "composer-resize"
            : "restore";
    return this.writeAndFinish(active, owner, top + active.anchor.offsetPx);
  }

  jumpToBlock(blockKey: string, blockTop: (blockKey: string) => number | undefined): boolean {
    const transaction = this.begin("jump", { kind: "block", blockKey, offsetPx: 0 });
    return transaction ? this.correctAnchor(transaction, blockTop) : false;
  }

  stageJumpToBlock(blockKey: string): ScrollTransaction | null {
    return this.begin("jump", { kind: "block", blockKey, offsetPx: 0 });
  }

  scrollToTail(): boolean {
    this.interactionVersion += 1;
    this.intentValue = "tail";
    this.anchorValue = { kind: "tail" };
    this.anchors.set(this.session, this.anchorValue);
    const transaction = this.begin("tail-sync", this.anchorValue);
    return Boolean(transaction && this.active && this.writeAndFinish(this.active, "tail-follow", Number.POSITIVE_INFINITY));
  }

  scheduleTailSync(): void {
    if (this.intentValue !== "tail" || this.userGesture || this.tailFrame !== null) return;
    const generation = this.generationValue;
    this.tailFrame = this.clock.requestAnimationFrame(() => {
      this.tailFrame = null;
      if (generation !== this.generationValue || this.intentValue !== "tail" || this.userGesture) return;
      const transaction = this.begin("tail-sync", { kind: "tail" });
      if (!transaction || !this.active) return;
      this.writeAndFinish(this.active, "tail-follow", Number.POSITIVE_INFINITY);
    });
  }

  writeUserControlled(owner: "selection-edge-scroll" | "custom-scrollbar" | "nested-scroll", offset: number): boolean {
    if (!this.write) return false;
    let active = this.active;
    if (!active || active.transaction.kind !== "selection") {
      const transaction = this.begin("selection", this.anchorValue);
      active = transaction ? this.active : null;
    }
    if (!active) return false;
    const result = this.write({
      session: this.session,
      generation: this.generationValue,
      transactionId: active.transaction.id,
      geometryRevision: this.geometryVersion,
      owner,
      intent: "reader",
      offset,
    });
    this.emitEvent(active.transaction, owner, offset, result.offset, result.accepted ? "accepted" : result.reason ?? "rejected");
    return result.accepted;
  }

  writeStructuralOffset(owner: "block-window-prepend", offset: number): boolean {
    if (!this.write || this.userGesture) return false;
    const transaction = this.begin("prepend", this.anchorValue);
    if (!transaction || !this.active) return false;
    const result = this.write({
      session: this.session,
      generation: this.generationValue,
      transactionId: transaction.id,
      geometryRevision: this.geometryVersion,
      owner,
      intent: this.intentValue,
      offset,
    });
    this.emitEvent(transaction, owner, offset, result.offset, result.accepted ? "accepted" : result.reason ?? "rejected");
    if (result.accepted) {
      this.writeTop = result.changed ? result.offset : null;
      this.finish(transaction.id, "committed", "committed");
    }
    return result.accepted;
  }

  reportAnomaly(outcome: "blank-viewport" | "invalid-geometry" | "missing-anchor"): void {
    this.anomalyCount += 1;
    this.emit({
      session: this.session,
      generation: this.generationValue,
      transaction: this.active?.transaction.id ?? 0,
      intent: this.intentValue,
      geometryRevision: this.geometryVersion,
      outcome,
    });
    if (this.anomalyCount >= 2) {
      this.safeModeValue = true;
      this.cancelActive("safe-mode");
    }
  }

  reportHealthyGeometry(): void {
    this.anomalyCount = 0;
  }

  private writeAndFinish(active: ActiveTransaction, owner: TranscriptScrollOwner, requested: number): boolean {
    if (!this.write || active.transaction.generation !== this.generationValue || this.userGesture) return false;
    const result = this.write({
      session: this.session,
      generation: this.generationValue,
      transactionId: active.transaction.id,
      geometryRevision: this.geometryVersion,
      owner,
      intent: this.intentValue,
      offset: requested,
    });
    this.emitEvent(active.transaction, owner, requested, result.offset, result.accepted ? "accepted" : result.reason ?? "rejected");
    if (result.accepted) {
      this.writeTop = result.changed ? result.offset : null;
      this.finish(active.transaction.id, "committed", "committed");
    }
    return result.accepted;
  }

  private cancelTailFrame(): void {
    if (this.tailFrame === null) return;
    this.clock.cancelAnimationFrame(this.tailFrame);
    this.tailFrame = null;
  }

  private clearNativeGestureLease(): void {
    if (this.nativeGestureTimer !== null) this.clock.clearTimeout(this.nativeGestureTimer);
    this.nativeGestureTimer = null;
  }

  private emitEvent(
    transaction: ScrollTransaction,
    owner?: TranscriptScrollOwner,
    requestedOffset?: number,
    acceptedOffset?: number,
    outcome: string = transaction.status,
  ): void {
    this.emit({
      session: this.session,
      generation: transaction.generation,
      transaction: transaction.id,
      owner,
      intent: this.intentValue,
      geometryRevision: this.geometryVersion,
      requestedOffset,
      acceptedOffset,
      outcome,
    });
  }
}
