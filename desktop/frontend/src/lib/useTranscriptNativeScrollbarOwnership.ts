import { useCallback, useRef, useState, type RefObject } from "react";
import type { TranscriptScrollEvent, TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";
import { CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS } from "./transcriptScrollDiagnosticProbe";
import type { TranscriptTailSettle } from "./transcriptTailSettle";

type NativeScrollbarTransaction = {
  pointerId: number;
  element: HTMLDivElement;
  lastTop: number;
  observedForwardProgress: boolean;
};

/** Owns the replaceable native-thumb candidate and every timer/DOM terminal. */
export function useTranscriptNativeScrollbarOwnership({
  scrollRef,
  modeRef,
  cancelReaderTransaction,
  deliverScroll,
  dispatch,
  tailSettle,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  cancelReaderTransaction: () => void;
  deliverScroll: (element?: HTMLDivElement) => void;
  dispatch: (event: TranscriptScrollEvent) => unknown;
  tailSettle: TranscriptTailSettle;
}) {
  const transactionRef = useRef<NativeScrollbarTransaction | null>(null);
  const [dragging, setDragging] = useState(false);

  const observe = useCallback((element: HTMLDivElement) => {
    const transaction = transactionRef.current;
    if (transaction?.element !== element) return;
    if (element.scrollTop > transaction.lastTop + 1) transaction.observedForwardProgress = true;
    transaction.lastTop = element.scrollTop;
  }, []);

  const begin = useCallback((pointerId: number, element: HTMLDivElement) => {
    const displaced = transactionRef.current;
    if (displaced?.element !== element) delete displaced?.element.dataset.nativeScrollbarDrag;
    cancelReaderTransaction();
    transactionRef.current = { pointerId, element, lastTop: element.scrollTop, observedForwardProgress: false };
    element.dataset.nativeScrollbarDrag = "true";
    setDragging(true);
    dispatch({ type: "NATIVE_SCROLLBAR_BEGIN" });
  }, [cancelReaderTransaction, dispatch]);

  const finish = useCallback((pointerId?: number) => {
    const transaction = transactionRef.current;
    if (!transaction || (pointerId !== undefined && transaction.pointerId !== pointerId)) return false;
    const currentElement = scrollRef.current === transaction.element;
    if (currentElement) deliverScroll(transaction.element);
    const claimTail = currentElement
      && transaction.observedForwardProgress
      && nativeTranscriptDistanceFromBottom(transaction.element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
    transactionRef.current = null;
    delete transaction.element.dataset.nativeScrollbarDrag;
    setDragging(false);
    dispatch({ type: "NATIVE_SCROLLBAR_END", claimTail });
    if (modeRef.current === "tail-follow") {
      tailSettle.schedule(false, CAPTURE_TRANSCRIPT_SCROLL_DIAGNOSTICS ? "native-scrollbar-release" : undefined);
    }
    return true;
  }, [deliverScroll, dispatch, modeRef, scrollRef, tailSettle]);
  const cancel = useCallback(() => {
    const transaction = transactionRef.current;
    if (!transaction) return false;
    transactionRef.current = null;
    delete transaction.element.dataset.nativeScrollbarDrag;
    setDragging(false);
    dispatch({ type: "NATIVE_SCROLLBAR_END", claimTail: false });
    return true;
  }, [dispatch]);
  const isActive = useCallback(() => transactionRef.current !== null, []);

  return { begin, cancel, finish, observe, isActive, dragging };
}
