import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import type { HistoryLoadTrigger, Item } from "./useController";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import { advanceSurfacePaintCommit, type SurfacePaintProgress } from "./navigationSurfaceTransition";
import {
  activeQuestionTurn,
  compactQuestionText,
  lastQuestionTurn,
  questionAnchorId,
  questionTurnsById,
  type QuestionAnchor,
  type QuestionAnchorPosition,
} from "./transcriptGrouping";
import { userRowKey } from "./transcriptRows";
import type { TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";

type PendingQuestionJump = {
  surfaceKey: string;
  turn: number;
  token: number;
  phase: "loading" | "landing" | "failed";
  anchorId?: string;
};

/** A late history/paint completion may only mutate the jump that owns it. */
export function settleQuestionJumpSurfaceState<T extends { token: number }>(
  current: T | null,
  completedToken: number,
  next: T | null,
): T | null {
  return current?.token === completedToken ? next : current;
}

export function useTranscriptQuestions(
  items: Item[],
  historyStartTurn: number,
  historyTotalTurns: number,
  scrollElement: HTMLElement | null,
  scrollToBottom: () => void,
) {
  const [questions, loadedByTurn, totalQuestions] = useMemo(() => {
    const loadedByTurn = new Map<number, QuestionAnchor>();
    let nextTurn = historyStartTurn > 0 ? historyStartTurn - 1 : 0;
    for (const item of items) {
      if (item.kind !== "user") continue;
      const turn = item.historyTurn != null && item.historyTurn > 0 ? item.historyTurn - 1 : nextTurn;
      loadedByTurn.set(turn, {
        id: item.id,
        text: compactQuestionText(item.text),
        turn,
        checkpointTurn: item.checkpointTurn,
      });
      nextTurn = Math.max(nextTurn, turn + 1);
    }
    return [
      Array.from(loadedByTurn.values()).sort((left, right) => left.turn - right.turn),
      loadedByTurn,
      Math.max(historyTotalTurns, nextTurn),
    ] as const;
  }, [historyStartTurn, historyTotalTurns, items]);
  const [activeQuestion, setActiveQuestion] = useState<number | null>(null);
  const activeQuestionFrame = useRef<number | null>(null);
  const questionTurnByAnchorId = useMemo(() => new Map(
    questions.map((question) => [questionAnchorId(question.id), question.turn]),
  ), [questions]);

  const syncActiveQuestion = useCallback(() => {
    if (!scrollElement || questions.length === 0) return;
    const scrollerTop = scrollElement.getBoundingClientRect().top;
    const positions: QuestionAnchorPosition[] = [];
    scrollElement.querySelectorAll<HTMLElement>("[data-question-anchor]").forEach((anchor) => {
      const turn = questionTurnByAnchorId.get(anchor.id);
      if (turn != null) positions.push({ turn, top: anchor.getBoundingClientRect().top - scrollerTop });
    });
    const next = activeQuestionTurn(positions);
    if (next != null) setActiveQuestion(next);
  }, [questionTurnByAnchorId, questions.length, scrollElement]);

  const scheduleActiveQuestionSync = useCallback(() => {
    if (activeQuestionFrame.current != null) return;
    activeQuestionFrame.current = requestAnimationFrame(() => {
      activeQuestionFrame.current = null;
      syncActiveQuestion();
    });
  }, [syncActiveQuestion]);

  useEffect(() => () => {
    if (activeQuestionFrame.current != null) cancelAnimationFrame(activeQuestionFrame.current);
  }, []);

  useEffect(() => {
    setActiveQuestion(questions[questions.length - 1]?.turn ?? null);
    scheduleActiveQuestionSync();
  }, [questions, scheduleActiveQuestionSync]);

  // A local question changes the tail id. Prepended history keeps it stable and
  // leaves viewport preservation to Virtuoso's firstItemIndex contract.
  const questionTailRef = useRef({ total: 0, lastId: "" });
  useEffect(() => {
    const lastId = questions[questions.length - 1]?.id ?? "";
    const previous = questionTailRef.current;
    questionTailRef.current = { total: totalQuestions, lastId };
    if (previous.total > 0 && totalQuestions > previous.total && lastId !== previous.lastId) scrollToBottom();
  }, [questions, scrollToBottom, totalQuestions]);

  const userTurn = useMemo(() => questionTurnsById(questions), [questions]);
  const turnForUser = useCallback((item: Extract<Item, { kind: "user" }>) => userTurn.get(item.id), [userTurn]);
  const lastTurn = useMemo(() => lastQuestionTurn(questions, userTurn), [questions, userTurn]);
  return [
    questions,
    loadedByTurn,
    totalQuestions,
    activeQuestion,
    setActiveQuestion,
    scheduleActiveQuestionSync,
    turnForUser,
    lastTurn,
  ] as const;
}

export function useTranscriptQuestionJump({
  questions,
  loadedByTurn,
  layoutSurfaceKey,
  rowIndexByKey,
  hasOlderHistory,
  loadingOlderHistory,
  olderHistoryError,
  running,
  scrollElement,
  onLoadOlderHistory,
  clearTranscriptSelection,
  invalidateAnchors,
  scrollToDataIndex,
  recoverPlacement,
  setActiveQuestion,
  rewindSignal,
}: {
  questions: QuestionAnchor[];
  loadedByTurn: Map<number, QuestionAnchor>;
  layoutSurfaceKey: string;
  rowIndexByKey: Map<string, number>;
  hasOlderHistory: boolean;
  loadingOlderHistory: boolean;
  olderHistoryError?: string;
  running: boolean;
  scrollElement: HTMLElement | null;
  onLoadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => boolean | Promise<boolean>;
  clearTranscriptSelection: (reason?: string) => void;
  invalidateAnchors: () => void;
  scrollToDataIndex: (index: number, behavior?: "auto" | "smooth") => void;
  recoverPlacement: (anchor: TranscriptLayoutAnchor) => void;
  setActiveQuestion: (turn: number | null) => void;
  rewindSignal: number;
}) {
  const [pendingQuestion, setPendingQuestion] = useState<PendingQuestionJump | null>(null);
  const pendingQuestionRef = useRef<PendingQuestionJump | null>(null);
  const questionJumpTokenRef = useRef(0);
  const olderRequestInFlightRef = useRef<string | null>(null);
  const replacePendingQuestion = useCallback((next: PendingQuestionJump | null) => {
    pendingQuestionRef.current = next;
    setPendingQuestion(next);
  }, []);
  const settlePendingQuestion = useCallback((token: number, outcome: "ready" | "degraded" | "failed" | "superseded") => {
    const current = pendingQuestionRef.current;
    if (!current || current.token !== token) return;
    const next = outcome === "failed" ? { ...current, phase: "failed" as const } : null;
    pendingQuestionRef.current = next;
    setPendingQuestion((value) => settleQuestionJumpSurfaceState(value, token, next));
    recordFrontendDiagnostic("transcript", "transcript.question-jump-terminal", { intent: token, outcome });
  }, []);
  const requestOlderHistory = useCallback(async (targetTurn?: number, retry = false, trigger: HistoryLoadTrigger = "retry"): Promise<boolean> => {
    if (!hasOlderHistory || loadingOlderHistory || running || !onLoadOlderHistory || (!retry && olderHistoryError)) return false;
    if (olderRequestInFlightRef.current === layoutSurfaceKey) return false;
    olderRequestInFlightRef.current = layoutSurfaceKey;
    try {
      // Await inside the try so the per-surface lease covers the backend/store
      // request itself. Returning the promise directly would run finally at
      // once, allowing the pending-jump effect to start a duplicate targeted
      // page before the controller publishes loadingOlderHistory.
      return await Promise.resolve(onLoadOlderHistory(targetTurn, trigger));
    } catch {
      // Controller-owned failures normally resolve false after publishing the
      // sanitized retry state. Treat an unexpected callback rejection the
      // same way so no fire-and-forget navigation promise is left unhandled.
      return false;
    } finally {
      if (olderRequestInFlightRef.current === layoutSurfaceKey) olderRequestInFlightRef.current = null;
    }
  }, [hasOlderHistory, layoutSurfaceKey, loadingOlderHistory, olderHistoryError, onLoadOlderHistory, running]);
  const requestQuestionHistory = useCallback((pending: PendingQuestionJump, retry: boolean, trigger: HistoryLoadTrigger) => {
    const alreadyInFlight = olderRequestInFlightRef.current === pending.surfaceKey;
    void requestOlderHistory(pending.turn + 1, retry, trigger).then((loaded) => {
      // A render can observe loading=false just before the active promise
      // settles. That duplicate attempt must not fail the surface owned by the
      // request already in flight; its authoritative result will drive the
      // next render. Every other rejected start is terminal and releases the
      // mask instead of leaving a permanent loading surface.
      if (!loaded && !alreadyInFlight && pendingQuestionRef.current?.token === pending.token) {
        settlePendingQuestion(pending.token, "failed");
      }
    });
  }, [requestOlderHistory, settlePendingQuestion]);
  const jumpToLoadedQuestion = useCallback((question: QuestionAnchor, behavior: "auto" | "smooth" = "smooth") => {
    const index = rowIndexByKey.get(userRowKey(question.id));
    if (index == null) return;
    document.getSelection()?.removeAllRanges();
    clearTranscriptSelection("question-navigation");
    invalidateAnchors();
    setActiveQuestion(question.turn);
    scrollToDataIndex(index, behavior);
  }, [clearTranscriptSelection, invalidateAnchors, rowIndexByKey, scrollToDataIndex, setActiveQuestion]);
  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    setActiveQuestion(question.turn);
    if (question.loaded !== false) {
      const superseded = pendingQuestionRef.current;
      if (superseded) settlePendingQuestion(superseded.token, "superseded");
      jumpToLoadedQuestion(question);
      return;
    }
    document.getSelection()?.removeAllRanges();
    clearTranscriptSelection("question-navigation");
    const pending = {
      surfaceKey: layoutSurfaceKey,
      turn: question.turn,
      token: questionJumpTokenRef.current + 1,
      phase: "loading" as const,
    };
    questionJumpTokenRef.current = pending.token;
    // Commit the opaque mask before a synchronous store/backend callback can
    // publish the first prepend. The target history may internally traverse
    // several valid windows, but none is a user-visible surface.
    flushSync(() => replacePendingQuestion(pending));
    recordFrontendDiagnostic("transcript", "transcript.question-jump-begin", { intent: pending.token });
    requestQuestionHistory(pending, true, "question-jump");
  }, [clearTranscriptSelection, jumpToLoadedQuestion, layoutSurfaceKey, replacePendingQuestion, requestQuestionHistory, setActiveQuestion, settlePendingQuestion]);

  useEffect(() => {
    if (!pendingQuestion || pendingQuestion.surfaceKey !== layoutSurfaceKey) return;
    if (pendingQuestion.phase === "failed") return;
    if (!loadingOlderHistory && olderHistoryError) {
      settlePendingQuestion(pendingQuestion.token, "failed");
      return;
    }
    if (pendingQuestion.phase !== "loading") return;
    const question = loadedByTurn.get(pendingQuestion.turn);
    if (question && !loadingOlderHistory) {
      const landing = { ...pendingQuestion, phase: "landing" as const, anchorId: questionAnchorId(question.id) };
      pendingQuestionRef.current = landing;
      setPendingQuestion((value) => settleQuestionJumpSurfaceState(value, pendingQuestion.token, landing));
      // The entire targeted paging sequence stays masked, so the only indexed
      // write is an immediate final placement after the authoritative page is
      // committed. Smooth animation would expose another intermediate path.
      jumpToLoadedQuestion(question, "auto");
      recoverPlacement({ mode: "manual", rowKey: userRowKey(question.id), offset: 0 });
    } else if (!loadingOlderHistory && !olderHistoryError) {
      requestQuestionHistory(pendingQuestion, false, "question-jump");
    }
  }, [jumpToLoadedQuestion, layoutSurfaceKey, loadedByTurn, loadingOlderHistory, olderHistoryError, pendingQuestion, recoverPlacement, requestQuestionHistory, settlePendingQuestion]);

  useEffect(() => {
    if (!pendingQuestion || pendingQuestion.surfaceKey !== layoutSurfaceKey || pendingQuestion.phase !== "landing") return;
    const { anchorId, token } = pendingQuestion;
    let frame: number | null = null;
    let cancelled = false;
    let progress: SurfacePaintProgress = { attempts: 0, stableFrames: 0 };
    const tick = () => {
      frame = null;
      if (cancelled || pendingQuestionRef.current?.token !== token) return;
      const target = anchorId ? document.getElementById(anchorId) : null;
      const mounted = Boolean(scrollElement && target && scrollElement.contains(target));
      let targetVisible = mounted;
      if (scrollElement && target) {
        const scrollerRect = scrollElement.getBoundingClientRect();
        const targetRect = target.getBoundingClientRect();
        // jsdom intentionally reports zero rects; native/browser surfaces must
        // additionally prove viewport intersection before releasing the mask.
        if (scrollerRect.height > 0 && targetRect.height > 0) {
          targetVisible = targetRect.bottom >= scrollerRect.top && targetRect.top <= scrollerRect.bottom;
        }
      }
      const geometryKey = scrollElement
        ? `${Math.round(scrollElement.clientHeight)}:${Math.round(scrollElement.scrollHeight)}:${Math.round(scrollElement.scrollTop)}`
        : undefined;
      const decision = advanceSurfacePaintCommit(progress, {
        rendered: mounted,
        placementReady: targetVisible,
        geometryReady: !loadingOlderHistory,
        geometryKey,
      });
      progress = decision.progress;
      if (decision.outcome) {
        settlePendingQuestion(token, decision.outcome);
        return;
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelled = true;
      if (frame !== null) cancelAnimationFrame(frame);
    };
  }, [layoutSurfaceKey, loadingOlderHistory, pendingQuestion, scrollElement, settlePendingQuestion]);

  useEffect(() => {
    const stale = pendingQuestionRef.current;
    if (stale && stale.surfaceKey !== layoutSurfaceKey) settlePendingQuestion(stale.token, "superseded");
    if (olderRequestInFlightRef.current !== layoutSurfaceKey) olderRequestInFlightRef.current = null;
  }, [layoutSurfaceKey, settlePendingQuestion]);

  const handleEarlierHistoryReached = useCallback(() => void requestOlderHistory(undefined, false, "viewport-user"), [requestOlderHistory]);
  const retryOlderHistory = useCallback(() => {
    const targetTurn = pendingQuestion?.surfaceKey === layoutSurfaceKey ? pendingQuestion.turn + 1 : undefined;
    if (pendingQuestion?.surfaceKey === layoutSurfaceKey && pendingQuestion.phase === "failed") {
      const retry = { ...pendingQuestion, phase: "loading" as const };
      pendingQuestionRef.current = retry;
      setPendingQuestion((value) => settleQuestionJumpSurfaceState(value, pendingQuestion.token, retry));
      requestQuestionHistory(retry, true, "retry");
      return;
    }
    void requestOlderHistory(targetTurn, true, "retry");
  }, [layoutSurfaceKey, pendingQuestion, requestOlderHistory, requestQuestionHistory]);

  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const index = rowIndexByKey.get(userRowKey(questions[questions.length - 1].id));
    if (index == null) return;
    invalidateAnchors();
    scrollToDataIndex(index);
    // Rewind is an edge-triggered signal; other values are read at that event.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

  const questionJumpSurface = pendingQuestion?.surfaceKey === layoutSurfaceKey && pendingQuestion.phase !== "failed"
    ? { token: pendingQuestion.token, phase: pendingQuestion.phase }
    : null;
  return [handleJumpToQuestion, handleEarlierHistoryReached, retryOlderHistory, questionJumpSurface] as const;
}
