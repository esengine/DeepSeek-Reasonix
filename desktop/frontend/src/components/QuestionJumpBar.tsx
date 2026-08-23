// QuestionJumpBar: the question navigator rail along the transcript edge.

import { useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent } from "react";
import { useT } from "../lib/i18n";
import type { QuestionAnchor } from "../lib/transcriptGrouping";

export const QUESTION_JUMP_MAX_MARKERS = 120;

export function sampledQuestionTurns(totalQuestions: number, activeTurn: number | null, limit = QUESTION_JUMP_MAX_MARKERS): number[] {
  const total = Math.max(0, Math.floor(totalQuestions));
  const maxMarkers = Math.max(2, Math.floor(limit));
  if (total <= maxMarkers) return Array.from({ length: total }, (_, turn) => turn);

  const turns = Array.from({ length: maxMarkers }, (_, index) => (
    Math.round(index * (total - 1) / (maxMarkers - 1))
  ));
  if (maxMarkers <= 2 || activeTurn == null || activeTurn <= 0 || activeTurn >= total - 1 || turns.includes(activeTurn)) return turns;

  let replaceIndex = 1;
  let closestDistance = Number.POSITIVE_INFINITY;
  for (let index = 1; index < turns.length - 1; index += 1) {
    const distance = Math.abs(turns[index] - activeTurn);
    if (distance < closestDistance) {
      closestDistance = distance;
      replaceIndex = index;
    }
  }
  turns[replaceIndex] = activeTurn;
  turns.sort((left, right) => left - right);
  return turns;
}

export function questionTurnFromRailY(clientY: number, top: number, height: number, totalQuestions: number): number | null {
  const total = Math.max(0, Math.floor(totalQuestions));
  if (total === 0 || !Number.isFinite(height) || height <= 0) return null;
  const ratio = Math.max(0, Math.min(1, (clientY - top) / height));
  return Math.min(total - 1, Math.floor(ratio * total));
}

export function QuestionJumpBar({
  loadedQuestions,
  totalQuestions,
  activeTurn,
  onJump,
}: {
  loadedQuestions: QuestionAnchor[];
  totalQuestions: number;
  activeTurn: number | null;
  onJump: (question: QuestionAnchor) => void;
}) {
  const t = useT();
  const total = Math.max(0, Math.floor(totalQuestions));
  const [hovered, setHovered] = useState<number | null>(null);
  const barRef = useRef<HTMLElement>(null);
  const railRef = useRef<HTMLDivElement>(null);
  const previewTop = useRef(0);
  const [showPreview, setShowPreview] = useState(false);

  const loadedByTurn = useMemo(() => {
    const loaded = new Map<number, QuestionAnchor>();
    for (const question of loadedQuestions) loaded.set(question.turn, question);
    return loaded;
  }, [loadedQuestions]);
  const active = activeTurn != null && activeTurn >= 0 && activeTurn < total
    ? activeTurn
    : total > 0 ? total - 1 : null;

  const markerTurns = useMemo(() => sampledQuestionTurns(total, active), [active, total]);
  const hoverIdx = hovered === null
    ? -1
    : markerTurns.reduce((closest, turn, index) => (
        closest < 0 || Math.abs(turn - hovered) < Math.abs(markerTurns[closest] - hovered) ? index : closest
      ), -1);

  const questionAt = (turn: number): QuestionAnchor => loadedByTurn.get(turn) ?? {
    id: `history-question-${turn + 1}`,
    text: t("questionNav.notLoaded", { n: turn + 1 }),
    turn,
    loaded: false,
  };
  const hoveredQuestion = hovered === null ? undefined : questionAt(hovered);

  const questionFromY = (clientY: number): { question: QuestionAnchor; previewY: number } | null => {
    const rail = railRef.current;
    const bar = barRef.current;
    if (!rail || !bar) return null;
    const railRect = rail.getBoundingClientRect();
    const turn = questionTurnFromRailY(clientY, railRect.top, railRect.height, total);
    if (turn === null) return null;
    const barRect = bar.getBoundingClientRect();
    const turnCenter = railRect.top - barRect.top + ((turn + 0.5) / total) * railRect.height;
    return {
      question: questionAt(turn),
      previewY: Math.max(0, Math.min(barRect.height, turnCenter)),
    };
  };

  const scrollTo = (question: QuestionAnchor) => {
    onJump(question);
  };

  const onMove = (event: ReactMouseEvent<HTMLDivElement>) => {
    const target = questionFromY(event.clientY);
    if (!target) return;
    previewTop.current = target.previewY;
    setHovered(target.question.turn);
    setShowPreview(true);
  };

  const onRailMouseDown = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    const target = questionFromY(event.clientY);
    if (!target) return;
    event.preventDefault();
    previewTop.current = target.previewY;
    setHovered(target.question.turn);
    setShowPreview(true);
    scrollTo(target.question);
  };

  const onRailKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (total === 0) return;
    const current = active ?? Math.max(0, total - 1);
    const page = Math.max(1, Math.round(total / 10));
    let next: number | null = null;
    switch (event.key) {
      case "ArrowUp":
      case "ArrowLeft": next = current - 1; break;
      case "ArrowDown":
      case "ArrowRight": next = current + 1; break;
      case "PageUp": next = current - page; break;
      case "PageDown": next = current + page; break;
      case "Home": next = 0; break;
      case "End": next = total - 1; break;
      case "Enter":
      case " ": next = current; break;
      default: return;
    }
    event.preventDefault();
    scrollTo(questionAt(Math.max(0, Math.min(total - 1, next))));
  };

  const dotProps = (idx: number, turn: number): { style: CSSProperties; "data-d"?: string } => {
    const isActive = active === turn;
    if (hoverIdx < 0) {
      return { style: { width: isActive ? 18 : 12, background: isActive ? "var(--accent)" : undefined } };
    }
    const d = Math.abs(idx - hoverIdx);
    const width = d === 0 ? 32 : d === 1 ? 20 : d === 2 ? 14 : isActive ? 18 : 12;
    const background = d <= 2 ? undefined : isActive ? "var(--accent)" : undefined;
    return {
      style: { width, transitionDelay: `${Math.min(d, 3) * 20}ms`, background },
      "data-d": d <= 2 ? String(d) : undefined,
    };
  };

  const density = markerTurns.length > 80 ? "packed" : markerTurns.length > 40 ? "compact" : "normal";
  const activeValue = active ?? Math.max(0, total - 1);

  return (
    <nav
      className="jump-bar"
      ref={barRef}
      aria-label={t("questionNav.label")}
      onMouseLeave={() => {
        setHovered(null);
        setShowPreview(false);
      }}
    >
      <div
        className="jump-scroll"
        ref={railRef}
        role="slider"
        tabIndex={0}
        aria-label={t("questionNav.label")}
        aria-orientation="vertical"
        aria-valuemin={1}
        aria-valuemax={Math.max(1, total)}
        aria-valuenow={activeValue + 1}
        aria-valuetext={t("questionNav.progress", { current: activeValue + 1, total })}
        data-density={density}
        onMouseMove={onMove}
        onMouseDown={onRailMouseDown}
        onKeyDown={onRailKeyDown}
      >
        {markerTurns.map((turn, index) => (
          <span
            className="jump-item"
            key={turn}
            data-turn={turn}
            data-loaded={loadedByTurn.has(turn) ? "true" : "false"}
            aria-hidden="true"
            style={{ top: `${((turn + 0.5) / total) * 100}%` }}
          >
            <span className="jump-dot" {...dotProps(index, turn)} />
          </span>
        ))}
      </div>
      {showPreview && hoveredQuestion && (
        <div className="jump-preview" style={{ top: previewTop.current }} role="tooltip">
          <span className="jump-text">{hoveredQuestion.text}</span>
        </div>
      )}
    </nav>
  );
}

export default QuestionJumpBar;
