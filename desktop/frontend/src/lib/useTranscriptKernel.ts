import { createContext, useContext, useCallback, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type RefObject } from "react";
import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";
import {
  TranscriptKernel,
  type TranscriptKernelClock,
  type ScrollTransactionKind,
  type LogicalAnchor,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
  type TranscriptViewportSnapshot,
} from "./transcriptKernel";
import { TranscriptViewportWriter } from "./transcriptViewportWriter";

const SCROLL_KEYS = new Set(["ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End", " "]);
// Native WebViews can leave a short gap between coalesced wheel batches while
// the platform compositor is still the authoritative scroll owner. Keep the
// lease beyond that boundary so deferred geometry is committed only after the
// native stream has actually gone idle.
const NATIVE_GESTURE_IDLE_MS = 320;
export const TranscriptKernelClockContext = createContext<TranscriptKernelClock | undefined>(undefined);
function blockTop(element: HTMLElement, key: string): number | undefined {
  const node = Array.from(element.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
    .find((candidate) => candidate.dataset.transcriptBlockKey === key);
  if (!node) return undefined;
  return node.getBoundingClientRect().top - element.getBoundingClientRect().top + element.scrollTop;
}

function readSnapshot(element: HTMLElement): TranscriptViewportSnapshot {
  const viewport = element.getBoundingClientRect();
  const top = element.scrollTop;
  return {
    scrollTop: top,
    scrollHeight: element.scrollHeight,
    clientHeight: element.clientHeight,
    visibleBlocks: Array.from(element.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
      .map((node) => {
        const rect = node.getBoundingClientRect();
        const contentTop = rect.top - viewport.top + top;
        return { key: node.dataset.transcriptBlockKey ?? "", top: contentTop, bottom: contentTop + rect.height };
      })
      .filter((block) => block.key && block.bottom >= top && block.top <= top + element.clientHeight)
      .sort((left, right) => left.top - right.top),
  };
}

export function useTranscriptKernel({
  sessionKey,
  geometryRevision,
}: {
  sessionKey: string;
  geometryRevision: string | number;
}) {
  const clock = useContext(TranscriptKernelClockContext);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);
  const kernelRef = useRef<TranscriptKernel | null>(null);
  const writerRef = useRef<TranscriptViewportWriter | null>(null);
  const [, setRevision] = useState(0);
  if (!kernelRef.current) {
    kernelRef.current = new TranscriptKernel({
      clock,
      emit: (event) => recordTranscriptScrollDiagnostic("kernel", event),
    });
    writerRef.current = new TranscriptViewportWriter();
  }
  const kernel = kernelRef.current;
  const writer = writerRef.current!;
  // 0 = idle, 1 = content pointer, 2 = native scrollbar thumb. The shared
  // state deduplicates PointerEvent + compatibility MouseEvent delivery.
  const pointerGestureRef = useRef(0);
  const observedTopRef = useRef(0);
  const inputRevisionRef = useRef(0);
  const prependAwaitingGeometryRef = useRef(false);
  const geometryWork = useRef<{ generation: number; cancel: () => void } | null>(null);
  const coverageRef = useRef(true);

  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const snapshot = useCallback(() => {
    const element = scrollRef.current;
    if (!element || ![element.scrollTop, element.scrollHeight, element.clientHeight].every(Number.isFinite)) return null;
    return readSnapshot(element);
  }, []);

  useLayoutEffect(() => {
    const disconnect = kernel.connectWriter(writer.write);
    return () => {
      kernel.detachSurface();
      geometryWork.current?.cancel();
      writer.attach(null, kernel.generation);
      disconnect();
    };
  }, [kernel, writer]);

  useLayoutEffect(() => {
    prependAwaitingGeometryRef.current = false;
    inputRevisionRef.current += 1;
    coverageRef.current = true;
    pointerGestureRef.current = 0;
    writer.freeze(false);
    const restored = kernel.replaceSurface(sessionKey);
    writer.attach(scrollRef.current, restored.generation);
    if (restored.anchor.kind === "block") kernel.begin("restore", restored.anchor);
    refresh();
  }, [kernel, refresh, sessionKey, writer]);

  const setScroller = useCallback((element: HTMLDivElement | null) => {
    scrollRef.current = element;
    observedTopRef.current = element?.scrollTop ?? 0;
    setScrollElement(element);
    writer.attach(element, kernel.generation);
  }, [kernel, writer]);

  const settleGeometry = useCallback(function settleGeometry(beforePaint = false) {
    if (!beforePaint && geometryWork.current?.generation === kernel.generation) return;
    geometryWork.current?.cancel();
    geometryWork.current = null;
    const commit = () => {
      geometryWork.current = null;
      const element = scrollRef.current;
      if (!element) return;
      const geometry = snapshot();
      const invalid = !geometry;
      const blank = !coverageRef.current || (element.getBoundingClientRect().height > 0
        && Number(element.dataset.transcriptBlockCount) > 0 && geometry?.visibleBlocks.length === 0);
      if (invalid || blank) {
        kernel.reportAnomaly(invalid ? "invalid-geometry" : "blank-viewport");
        // One clock-owned re-observation, not a scroll retry loop. The second
        // consecutive fault latches safety until replaceSurface resets it.
        if (!kernel.safeMode) kernel.afterCurrentGenerationPaint(settleGeometry);
        refresh();
        return;
      }
      kernel.reportHealthyGeometry();
      kernel.advanceGeometry();
      const transaction = kernel.activeTransaction ?? (kernel.intent === "tail" ? kernel.begin("tail-sync", { kind: "tail" }) : null);
      if (transaction && element && (transaction.kind !== "prepend" || !prependAwaitingGeometryRef.current)) {
        kernel.correctAnchor(transaction, (key) => blockTop(element, key));
      }
    };
    if (beforePaint) commit();
    else {
      const cancel = kernel.afterCurrentGenerationPaint(commit);
      geometryWork.current = { generation: kernel.generation, cancel };
    }
  }, [kernel, refresh, snapshot]);

  const commitViewportGeometry = useCallback((covered?: boolean, beforePaint = false) => {
    if (covered !== undefined) coverageRef.current = covered;
    prependAwaitingGeometryRef.current = false;
    settleGeometry(beforePaint);
  }, [settleGeometry]);

  const beginAnchorRestore = useCallback((anchor?: LogicalAnchor) => {
    const restoreAnchor = anchor ?? kernel.anchor;
    if (kernel.userGestureActive || kernel.intent !== "reader" || restoreAnchor.kind !== "block") return null;
    const transaction = kernel.activeTransaction ?? kernel.begin("restore", restoreAnchor);
    if (transaction) refresh();
    return transaction;
  }, [kernel, refresh]);

  useLayoutEffect(() => {
    settleGeometry();
  }, [geometryRevision, settleGeometry]);

  const beginStructural = useCallback((kind: Exclude<ScrollTransactionKind, "jump" | "selection" | "tail-sync">) => {
    // Structural geometry preserves the input-owned logical anchor; a new
    // bottom gap after layout cannot turn tail follow into reader intent.
    const anchor = kernel.anchor;
    if (kind === "prepend") prependAwaitingGeometryRef.current = true;
    const transaction = kernel.begin(kind, anchor);
    refresh();
    return transaction;
  }, [kernel, refresh]);

  const beginGesture = useCallback((owner: "selection" | "native" = "native") => {
    const current = snapshot();
    if (!current) return;
    inputRevisionRef.current += 1;
    kernel.beginUserGesture(current, owner);
    refresh();
  }, [kernel, refresh, snapshot]);

  const finishGesture = useCallback((resumed: ReturnType<TranscriptKernel["endUserGesture"]>) => {
    inputRevisionRef.current += 1;
    writer.freeze(false);
    pointerGestureRef.current = 0;
    if (resumed) kernel.afterCurrentGenerationPaint(settleGeometry);
    refresh();
  }, [kernel, refresh, settleGeometry, writer]);

  const endGesture = useCallback(() => {
    finishGesture(kernel.endUserGesture());
  }, [finishGesture, kernel]);

  const renewGestureLease = useCallback(() => {
    const current = snapshot();
    if (!current) return;
    kernel.renewNativeGesture(current, NATIVE_GESTURE_IDLE_MS, finishGesture);
    refresh();
  }, [finishGesture, kernel, refresh, snapshot]);

  const claimNativeInput = useCallback(() => {
    inputRevisionRef.current += 1;
    renewGestureLease();
  }, [renewGestureLease]);

  const releaseNativeInput = useCallback(() => {
    writer.freeze(false);
    pointerGestureRef.current = 0;
    claimNativeInput();
  }, [claimNativeInput, writer]);

  const onScroll = useCallback(() => {
    const current = snapshot();
    if (!current) return null;
    const towardHistory = current.scrollTop < observedTopRef.current;
    observedTopRef.current = current.scrollTop;
    const nativeOwned = kernel.observeNativeScroll(current);
    if (!nativeOwned) return null;
    // Wheel delivery and native scrolling are not synchronous on every
    // engine. Renew the same lease until the final native scroll event so the
    // browser's final position remains authoritative.
    if (kernel.nativeGestureLeaseActive) renewGestureLease();
    refresh();
    return towardHistory && kernel.userGestureActive;
  }, [kernel, refresh, renewGestureLease, snapshot]);

  const onPointerDownCapture = useCallback((event: { clientX: number; pointerType?: string }) => {
    // Touch has its own start/end stream. Its compatibility pointerup must
    // not schedule a mouse release that later cancels touch momentum.
    if (event.pointerType === "touch") return;
    if (pointerGestureRef.current) return;
    const element = scrollRef.current;
    if (!element) return;
    const rect = element.getBoundingClientRect();
    const nativeThumb = event.clientX >= rect.right - 18;
    pointerGestureRef.current = nativeThumb ? 2 : 1;
    writer.freeze(nativeThumb);
    beginGesture();
    const inputRevision = inputRevisionRef.current;
    const terminalEvents = ["pointerup", "pointercancel", "mouseup"] as const;
    const generation = kernel.generation;
    const finish = () => {
      terminalEvents.forEach((type) => window.removeEventListener(type, finish, true));
      if (generation === kernel.generation && inputRevision === inputRevisionRef.current) {
        pointerGestureRef.current = 0;
        writer.freeze(false);
      }
      if (generation === kernel.generation) kernel.afterCurrentGenerationPaint(() => {
        if (inputRevision !== inputRevisionRef.current) return;
        if (nativeThumb) {
          releaseNativeInput();
        } else {
          const current = snapshot();
          if (current && current.scrollTop !== observedTopRef.current) onScroll();
          endGesture();
        }
      });
    };
    terminalEvents.forEach((type) => window.addEventListener(type, finish, true));
  }, [beginGesture, endGesture, kernel, onScroll, releaseNativeInput, snapshot, writer]);

  const onKeyDownCapture = useCallback((event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (SCROLL_KEYS.has(event.key)) claimNativeInput();
  }, [claimNativeInput]);


  const scrollToBottom = useCallback(() => {
    endGesture();
    kernel.cancelActive("jump-to-bottom");
    kernel.scrollToTail();
    refresh();
  }, [endGesture, kernel, refresh]);

  const jumpToBlock = useCallback((key: string) => {
    const element = scrollRef.current;
    if (!element) return false;
    const mountedTop = blockTop(element, key);
    const accepted = mountedTop == null
      ? Boolean(kernel.stageJumpToBlock(key))
      : kernel.jumpToBlock(key, (blockKey) => blockTop(element, blockKey));
    refresh();
    return accepted;
  }, [kernel, refresh]);

  const setScrollMode = useCallback((mode: TranscriptScrollMode) => {
    if (mode === "selection") beginGesture("selection");
    else if (mode === "manual") endGesture();
    else if (mode === "tail-follow") scrollToBottom();
    else beginStructural("restore");
  }, [beginGesture, beginStructural, endGesture, scrollToBottom]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number) => {
    if (owner === "block-window-prepend") {
      const accepted = kernel.writeStructuralOffset(owner, top);
      refresh();
      return accepted;
    }
    if (owner !== "selection-edge-scroll" && owner !== "custom-scrollbar" && owner !== "nested-scroll") return false;
    const accepted = kernel.writeUserControlled(owner, top);
    refresh();
    return accepted;
  }, [kernel, refresh]);

  const geometry = snapshot();
  const isAtBottom = geometry
    ? geometry.scrollHeight - geometry.clientHeight - geometry.scrollTop <= 4
    : kernel.intent === "tail";

  return {
    scrollRef: scrollRef as RefObject<HTMLDivElement | null>,
    scrollElement,
    setScroller,
    kernel,
    writer,
    intent: kernel.intent,
    isAtBottom,
    safeMode: kernel.safeMode,
    nativeScrollbarDragging: pointerGestureRef.current === 2,
    snapshot,
    beginStructural,
    beginAnchorRestore,
    commitViewportGeometry,
    beginGesture,
    endGesture,
    settleGeometry,
    scrollToBottom,
    jumpToBlock,
    setScrollMode,
    writeOffset,
    onScroll,
    onPointerDownCapture,
    onWheelCapture: claimNativeInput,
    onTouchStartCapture: beginGesture,
    onTouchEndCapture: releaseNativeInput,
    onKeyDownCapture,
  };
}
