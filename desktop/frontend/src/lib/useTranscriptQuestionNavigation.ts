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

export function useTranscriptQuestions(
  items: Item[],
  historyStartTurn: number,
  historyTotalTurns: number,
  scrollElement: HTMLElement | null,
  scrollToBottom: () => void,
) {
  const [questions, loadedByTurn, totalQuestions] = useMemo(() => {
    const loaded = new Map<number, QuestionAnchor>();
    let nextTurn = historyStartTurn > 0 ? historyStartTurn - 1 : 0;
    for (const item of items) {
      if (item.kind !== "user") continue;
      const turn = item.historyTurn != null && item.historyTurn > 0 ? item.historyTurn - 1 : nextTurn;
      loaded.set(turn, { id: item.id, text: compactQuestionText(item.text), turn, checkpointTurn: item.checkpointTurn });
      nextTurn = Math.max(nextTurn, turn + 1);
    }
    return [Array.from(loaded.values()).sort((a, b) => a.turn - b.turn), loaded, Math.max(historyTotalTurns, nextTurn)] as const;
  }, [historyStartTurn, historyTotalTurns, items]);
  const [activeQuestion, setActiveQuestion] = useState<number | null>(null);
  const frameRef = useRef<number | null>(null);
  const turnsByAnchor = useMemo(() => new Map(questions.map((question) => [questionAnchorId(question.id), question.turn])), [questions]);

  const sync = useCallback(() => {
    if (!scrollElement || questions.length === 0) return;
    const top = scrollElement.getBoundingClientRect().top;
    const positions: QuestionAnchorPosition[] = [];
    scrollElement.querySelectorAll<HTMLElement>("[data-question-anchor]").forEach((anchor) => {
      const turn = turnsByAnchor.get(anchor.id);
      if (turn != null) positions.push({ turn, top: anchor.getBoundingClientRect().top - top });
    });
    const active = activeQuestionTurn(positions);
    if (active != null) setActiveQuestion(active);
  }, [questions.length, scrollElement, turnsByAnchor]);
  const scheduleSync = useCallback(() => {
    if (frameRef.current != null) return;
    frameRef.current = requestAnimationFrame(() => { frameRef.current = null; sync(); });
  }, [sync]);
  useEffect(() => () => { if (frameRef.current != null) cancelAnimationFrame(frameRef.current); }, []);
  useEffect(() => {
    setActiveQuestion(questions[questions.length - 1]?.turn ?? null);
    scheduleSync();
  }, [questions, scheduleSync]);

  const tailRef = useRef({ total: 0, lastId: "" });
  useEffect(() => {
    const lastId = questions[questions.length - 1]?.id ?? "";
    const previous = tailRef.current;
    tailRef.current = { total: totalQuestions, lastId };
    if (previous.total > 0 && totalQuestions > previous.total && lastId !== previous.lastId) scrollToBottom();
  }, [questions, scrollToBottom, totalQuestions]);

  const userTurns = useMemo(() => questionTurnsById(questions), [questions]);
  const turnForUser = useCallback((item: Extract<Item, { kind: "user" }>) => userTurns.get(item.id), [userTurns]);
  return [
    questions, loadedByTurn, totalQuestions, activeQuestion, setActiveQuestion,
    scheduleSync, turnForUser, lastQuestionTurn(questions, userTurns),
  ] as const;
}
