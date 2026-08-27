import {
  isSubstantialTranscriptDisplacement,
  type TranscriptScrollEvent,
  type TranscriptScrollState,
} from "./transcriptScrollArbiter";
import {
  hasTranscriptScrollableRange,
  nativeTranscriptDistanceFromBottom,
  TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
} from "./transcriptScrollGeometry";

type MutableRef<T> = { current: T };
type NativeExtent = { scrollHeight: number; clientHeight: number };
// Native ranges can settle well before a loaded CI WebView mounts LAST. Keep
// the explicit bottom-release proof bounded to roughly two seconds at 60fps.
export const MAX_TAIL_MOUNT_CHECKS = 120;
export const MAX_TAIL_MOUNT_HOLD_MS = 2_000;

function tailIsMounted(element: HTMLElement): boolean {
  if (element.querySelector("[data-live-region='true']")) return true;
  const totalRows = Number.parseInt(element.dataset.transcriptRowCount ?? "", 10);
  const firstItemIndex = Number.parseInt(element.dataset.transcriptFirstItemIndex ?? "", 10);
  if (!Number.isFinite(totalRows) || !Number.isFinite(firstItemIndex)) {
    return element.querySelector(".transcript__row") !== null;
  }
  if (totalRows <= 0) return false;
  const tailIndex = firstItemIndex + totalRows - 1;
  return Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-item-index]"))
    .some((row) => Number.parseInt(row.dataset.itemIndex ?? "", 10) === tailIndex);
}

function extentChanged(held: NativeExtent, element: HTMLElement): boolean {
  return Math.abs(held.scrollHeight - element.scrollHeight) > 1
    || Math.abs(held.clientHeight - element.clientHeight) > 1;
}

export type TranscriptReaderBottomHold = readonly [
  cancel: () => void,
  deliver: (element: HTMLDivElement, provedBottom?: boolean) => boolean,
];

export function createTranscriptReaderBottomHold({
  scrollRef,
  stateRef,
  generationRef,
  deliverScrollRef,
  dispatch,
}: {
  scrollRef: MutableRef<HTMLDivElement | null>;
  stateRef: MutableRef<TranscriptScrollState>;
  generationRef: MutableRef<number>;
  deliverScrollRef: MutableRef<((element?: HTMLDivElement) => void) | null>;
  dispatch: (event: TranscriptScrollEvent) => void;
}): TranscriptReaderBottomHold {
  let frame: number | null = null;
  let heldExtent: NativeExtent | null = null;
  let totalChecks = 0;
  let startedAt: number | null = null;
  let nativeProof = false;

  const cancel = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
    heldExtent = null;
    totalChecks = 0;
    startedAt = null;
    nativeProof = false;
  };

  const deliver = (element: HTMLDivElement, provedBottom = false) => {
    if (provedBottom && totalChecks === 0) {
      totalChecks = 1;
      startedAt = Date.now();
      nativeProof = true;
    }
    const distance = nativeTranscriptDistanceFromBottom(element);
    const atBottom = distance <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
    const tailMounted = tailIsMounted(element);
    if (heldExtent && extentChanged(heldExtent, element)) {
      // The earlier bottom sample belongs to a different native extent.
      if (frame !== null) cancelAnimationFrame(frame);
      frame = null;
      heldExtent = null;
      // A released native thumb is an explicit destination and retains its
      // bounded LAST fallback while the range is measured. An ordinary wheel
      // only proved the old physical extent: if that extent grows, discard
      // the stale proof so it cannot jump through an unmounted range and
      // expose a blank frame.
      if (!nativeProof) {
        totalChecks = 0;
        startedAt = null;
      }
      dispatch({
        type: "SCROLL_DELIVERED",
        atBottom: false,
        scrollable: hasTranscriptScrollableRange(element),
        substantial: isSubstantialTranscriptDisplacement(distance),
        tailMounted,
      });
    }
    dispatch({
      type: "SCROLL_DELIVERED",
      atBottom,
      scrollable: hasTranscriptScrollableRange(element),
      substantial: isSubstantialTranscriptDisplacement(distance),
      tailMounted,
    });

    const state = stateRef.current;
    const activeClaim = state.mode === "reader-gesture"
      && state.readerIntent
      && state.readerIntentCanClaimTail;
    if (
      (atBottom || totalChecks)
      && activeClaim
      && frame === null
    ) {
      if (
        totalChecks >= MAX_TAIL_MOUNT_CHECKS
        || (startedAt !== null && Date.now() - startedAt >= MAX_TAIL_MOUNT_HOLD_MS)
      ) {
        // The native thumb proved the physical end, but a loaded WebView can
        // leave LAST unmounted or keep revising its extent beyond the passive
        // observation budget. Hand that bounded failure to the existing jump-tail
        // transaction instead of abandoning the reader in manual mode. This
        // is one arbiter-owned command after release, never a direct write.
        cancel();
        dispatch({ type: "JUMP_TO_BOTTOM" });
        return false;
      }
      const generation = generationRef.current;
      startedAt ??= Date.now();
      heldExtent = { scrollHeight: element.scrollHeight, clientHeight: element.clientHeight };
      totalChecks += 1;
      frame = requestAnimationFrame(() => {
        const held = heldExtent;
        frame = null;
        heldExtent = null;
        const current = stateRef.current;
        if (
          generationRef.current !== generation
          || scrollRef.current !== element
          || current.mode !== "reader-gesture"
          || !current.readerIntent
          || !current.readerIntentCanClaimTail
        ) return;
        if (held && extentChanged(held, element)) {
          const currentDistance = nativeTranscriptDistanceFromBottom(element);
          if (!nativeProof) {
            totalChecks = 0;
            startedAt = null;
          }
          dispatch({
            type: "SCROLL_DELIVERED",
            atBottom: false,
            scrollable: hasTranscriptScrollableRange(element),
            substantial: isSubstantialTranscriptDisplacement(currentDistance),
            tailMounted: tailIsMounted(element),
          });
        }
        deliverScrollRef.current?.(element);
      });
    } else if (!activeClaim) {
      cancel();
    }
    // An explicit native-thumb bottom proof owns its bounded tail-mount
    // transaction. The ordinary reader idle timer must not end that intent
    // before LAST mounts or the bounded jump fallback runs.
    return nativeProof && activeClaim;
  };

  return [cancel, deliver];
}
