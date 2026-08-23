import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Item } from "./useController";
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

const HISTORY_AUTO_COMPLETE_TURNS = 60;

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
  suppressAutoComplete = false,
  onLoadOlderHistory,
  clearTranscriptSelection,
  invalidateAnchors,
  scrollToDataIndex,
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
  /** Avoid background history paging while a navigation surface is hidden. */
  suppressAutoComplete?: boolean;
  onLoadOlderHistory?: (targetTurn?: number) => boolean | Promise<boolean>;
  clearTranscriptSelection: (reason?: string) => void;
  invalidateAnchors: () => void;
  scrollToDataIndex: (index: number, behavior?: "auto" | "smooth") => void;
  setActiveQuestion: (turn: number | null) => void;
  rewindSignal: number;
}) {
  const [pendingQuestion, setPendingQuestion] = useState<{ surfaceKey: string; turn: number } | null>(null);
  const olderRequestInFlightRef = useRef<string | null>(null);
  const requestOlderHistory = useCallback(async (targetTurn?: number, retry = false): Promise<boolean> => {
    if (!hasOlderHistory || loadingOlderHistory || running || !onLoadOlderHistory || (!retry && olderHistoryError)) return false;
    if (olderRequestInFlightRef.current === layoutSurfaceKey) return false;
    olderRequestInFlightRef.current = layoutSurfaceKey;
    try {
      return onLoadOlderHistory(targetTurn);
    } finally {
      if (olderRequestInFlightRef.current === layoutSurfaceKey) olderRequestInFlightRef.current = null;
    }
  }, [hasOlderHistory, layoutSurfaceKey, loadingOlderHistory, olderHistoryError, onLoadOlderHistory, running]);
  const jumpToLoadedQuestion = useCallback((question: QuestionAnchor) => {
    const index = rowIndexByKey.get(userRowKey(question.id));
    if (index == null) return;
    document.getSelection()?.removeAllRanges();
    clearTranscriptSelection("question-navigation");
    invalidateAnchors();
    setActiveQuestion(question.turn);
    scrollToDataIndex(index, "smooth");
  }, [clearTranscriptSelection, invalidateAnchors, rowIndexByKey, scrollToDataIndex, setActiveQuestion]);
  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    setActiveQuestion(question.turn);
    if (question.loaded !== false) {
      setPendingQuestion(null);
      jumpToLoadedQuestion(question);
      return;
    }
    document.getSelection()?.removeAllRanges();
    clearTranscriptSelection("question-navigation");
    setPendingQuestion({ surfaceKey: layoutSurfaceKey, turn: question.turn });
    void requestOlderHistory(question.turn + 1, true);
  }, [clearTranscriptSelection, jumpToLoadedQuestion, layoutSurfaceKey, requestOlderHistory, setActiveQuestion]);

  useEffect(() => {
    if (!pendingQuestion || pendingQuestion.surfaceKey !== layoutSurfaceKey) return;
    const question = loadedByTurn.get(pendingQuestion.turn);
    if (question) {
      setPendingQuestion(null);
      jumpToLoadedQuestion(question);
    } else if (!loadingOlderHistory && !olderHistoryError) {
      void requestOlderHistory(pendingQuestion.turn + 1);
    }
  }, [jumpToLoadedQuestion, layoutSurfaceKey, loadedByTurn, loadingOlderHistory, olderHistoryError, pendingQuestion, requestOlderHistory]);

  useEffect(() => {
    setPendingQuestion(null);
    if (olderRequestInFlightRef.current !== layoutSurfaceKey) olderRequestInFlightRef.current = null;
  }, [layoutSurfaceKey]);

  const earlierTurnsRemaining = questions[0]?.turn ?? 0;
  useEffect(() => {
    if (suppressAutoComplete) return;
    if (earlierTurnsRemaining > 0 && earlierTurnsRemaining < HISTORY_AUTO_COMPLETE_TURNS) void requestOlderHistory();
  }, [earlierTurnsRemaining, requestOlderHistory, suppressAutoComplete]);
  const handleEarlierHistoryReached = useCallback(() => void requestOlderHistory(), [requestOlderHistory]);
  const retryOlderHistory = useCallback(() => {
    const targetTurn = pendingQuestion?.surfaceKey === layoutSurfaceKey ? pendingQuestion.turn + 1 : undefined;
    void requestOlderHistory(targetTurn, true);
  }, [layoutSurfaceKey, pendingQuestion, requestOlderHistory]);

  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const index = rowIndexByKey.get(userRowKey(questions[questions.length - 1].id));
    if (index == null) return;
    invalidateAnchors();
    scrollToDataIndex(index);
    // Rewind is an edge-triggered signal; other values are read at that event.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

  return [handleJumpToQuestion, handleEarlierHistoryReached, retryOlderHistory] as const;
}
