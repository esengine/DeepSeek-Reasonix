import { useEffect, useMemo, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { QuestionAnswer, WireAsk, WireAskQuestion } from "../lib/types";
import { PromptAction, PromptBadge, PromptDetailToggle, PromptShelf } from "./PromptShelf";

// AskCard renders the `ask` tool as a compact prompt shelf near the composer. It
// walks multi-question asks one at a time; single-select answers advance
// immediately, while multi-select and typed answers wait for explicit confirmation.
export function AskCard({
  ask,
  onAnswer,
  onDismiss,
}: {
  ask: WireAsk;
  onAnswer: (id: string, answers: QuestionAnswer[]) => void;
  onDismiss: () => void;
}) {
  const t = useT();
  // Per-question state: selected option labels, and an optional typed answer.
  const [sel, setSel] = useState<Record<string, string[]>>({});
  const [custom, setCustom] = useState<Record<string, string>>({});
  const [active, setActive] = useState(0);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const advanceTimer = useRef<number | null>(null);

  const questions = ask.questions;
  const q = questions[Math.min(active, questions.length - 1)];
  const isLast = active >= questions.length - 1;
  const progress = `${Math.min(active + 1, questions.length)}/${questions.length}`;
  const hasMultipleQuestions = questions.length > 1;

  useEffect(() => {
    shelfRef.current?.focus();
    setSel({});
    setCustom({});
    setActive(0);
    setDetailsOpen(false);
    if (advanceTimer.current != null) window.clearTimeout(advanceTimer.current);
  }, [ask.id]);

  useEffect(() => {
    return () => {
      if (advanceTimer.current != null) window.clearTimeout(advanceTimer.current);
    };
  }, []);

  const answersFrom = (
    nextSel: Record<string, string[]> = sel,
    nextCustom: Record<string, string> = custom,
  ): QuestionAnswer[] =>
    questions.map((question) => ({
      questionId: question.id,
      selected: nextCustom[question.id]?.trim() ? [nextCustom[question.id].trim()] : (nextSel[question.id] ?? []),
    }));

  const answerLabel = (question: WireAskQuestion) => {
    const typed = custom[question.id]?.trim();
    if (typed) return typed;
    return (sel[question.id] ?? []).join(", ");
  };

  const answered = (question: WireAskQuestion) =>
    (sel[question.id]?.length ?? 0) > 0 || (custom[question.id]?.trim() ?? "") !== "";

  const currentAnswered = q ? answered(q) : false;

  const finishOrAdvance = (nextSel = sel, nextCustom = custom) => {
    if (advanceTimer.current != null) {
      window.clearTimeout(advanceTimer.current);
      advanceTimer.current = null;
    }
    if (isLast) {
      onAnswer(ask.id, answersFrom(nextSel, nextCustom));
      return;
    }
    setDetailsOpen(false);
    setActive((i) => Math.min(i + 1, questions.length - 1));
  };

  const toggle = (question: WireAskQuestion, label: string) => {
    const nextCustom = { ...custom, [question.id]: "" };
    const cur = sel[question.id] ?? [];
    const nextSel = question.multi
      ? { ...sel, [question.id]: cur.includes(label) ? cur.filter((x) => x !== label) : [...cur, label] }
      : { ...sel, [question.id]: [label] };

    setCustom(nextCustom);
    setSel(nextSel);

    if (!question.multi) {
      if (advanceTimer.current != null) window.clearTimeout(advanceTimer.current);
      advanceTimer.current = window.setTimeout(() => finishOrAdvance(nextSel, nextCustom), 140);
    }
  };

  const setTyped = (question: WireAskQuestion, text: string) => {
    setCustom((c) => ({ ...c, [question.id]: text }));
    if (text.trim()) setSel((s) => ({ ...s, [question.id]: [] }));
  };

  const goBack = () => {
    if (advanceTimer.current != null) {
      window.clearTimeout(advanceTimer.current);
      advanceTimer.current = null;
    }
    setDetailsOpen(false);
    setActive((i) => Math.max(0, i - 1));
  };

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;

      if (event.key === "Escape") {
        event.preventDefault();
        onDismiss();
        return;
      }
      if ((event.key === "ArrowLeft" || event.key === "Backspace") && active > 0) {
        event.preventDefault();
        goBack();
        return;
      }

      const index = Number(event.key) - 1;
      if (!Number.isInteger(index) || index < 0 || index >= q.options.length) return;
      event.preventDefault();
      toggle(q, q.options[index].label);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [active, custom, onDismiss, q, sel]);

  const answeredSummary = useMemo(
    () =>
      questions
        .slice(0, active)
        .map((question) => answerLabel(question))
        .filter(Boolean),
    [active, custom, questions, sel],
  );

  if (!q) return null;

  return (
    <PromptShelf
      barRef={shelfRef}
      titleId="ask-shelf-title"
      title={t("ask.title")}
      actionsWrap
      badges={
        <>
          {q.header && <PromptBadge>{q.header}</PromptBadge>}
          {hasMultipleQuestions && <PromptBadge>{t("ask.questionProgress", { progress })}</PromptBadge>}
        </>
      }
      meta={q.prompt}
      actions={
        <>
          {active > 0 && (
            <button className="prompt-action prompt-action--quiet" onClick={goBack}>
              <span className="prompt-action__label">{t("ask.back")}</span>
            </button>
          )}
          {q.options.map((o, index) => {
            const on = (sel[q.id] ?? []).includes(o.label);
            return (
              <PromptAction
                key={o.label}
                keyLabel={String(index + 1)}
                label={o.label}
                onClick={() => toggle(q, o.label)}
                selected={on}
              />
            );
          })}
          {q.multi && (
            <button className="prompt-action prompt-action--selected" onClick={() => finishOrAdvance()} disabled={!currentAnswered}>
              <span className="prompt-action__label">{isLast ? t("common.submit") : t("ask.next")}</span>
            </button>
          )}
          <PromptDetailToggle
            open={detailsOpen}
            label={t("ask.details")}
            openLabel={t("ask.hideDetails")}
            onClick={() => setDetailsOpen((open) => !open)}
          />
          <button className="prompt-action prompt-action--quiet" onClick={onDismiss}>
            <span className="prompt-action__label">{t("ask.justChat")}</span>
          </button>
        </>
      }
      crumbs={
        answeredSummary.length > 0 && (
        <div className="ask-shelf__crumbs">
          {answeredSummary.map((answer, index) => (
            <span className="ask-shelf__crumb" key={`${index}-${answer}`}>
              {index + 1}. {answer}
            </span>
          ))}
        </div>
        )
      }
    >
      {detailsOpen && (
        <>
          <div className="ask-shelf__detail-list">
            {q.options.map((o) => (
              <div className="ask-shelf__detail" key={o.label}>
                <span className="ask-shelf__detail-label">{o.label}</span>
                {o.description && <span className="ask-shelf__detail-desc">{o.description}</span>}
              </div>
            ))}
          </div>
          <div className="ask-shelf__custom-row">
            <input
              className="ask-shelf__custom"
              placeholder={t("ask.customPlaceholder")}
              value={custom[q.id] ?? ""}
              onChange={(e) => setTyped(q, e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && currentAnswered) finishOrAdvance();
                e.stopPropagation();
              }}
            />
            <div className="ask-shelf__panel-actions">
              {active > 0 && (
                <button className="btn" onClick={goBack}>
                  {t("ask.back")}
                </button>
              )}
              <button className="btn" onClick={onDismiss}>
                {t("ask.justChat")}
              </button>
              {(q.multi || custom[q.id]?.trim()) && (
                <button className="btn btn--primary" onClick={() => finishOrAdvance()} disabled={!currentAnswered}>
                  {isLast ? t("common.submit") : t("ask.next")}
                </button>
              )}
            </div>
          </div>
        </>
      )}
    </PromptShelf>
  );
}
