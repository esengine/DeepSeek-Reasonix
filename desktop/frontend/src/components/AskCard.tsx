import { useEffect, useMemo, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { QuestionAnswer, WireAsk, WireAskQuestion } from "../lib/types";
import { PromptAction, PromptHeaderAction, PromptShelf } from "./PromptShelf";

// AskCard renders the `ask` tool as a compact prompt shelf near the composer. It
// walks multi-question asks one at a time; single-select answers advance
// immediately, while multi-select and typed answers wait for explicit confirmation.
export function AskCard({
  ask,
  onAnswer,
  onDismiss,
  onStop,
}: {
  ask: WireAsk;
  onAnswer: (id: string, answers: QuestionAnswer[]) => void;
  onDismiss: () => void;
  onStop: () => void;
}) {
  const t = useT();
  // Per-question state: selected option labels, and an optional typed answer.
  const [sel, setSel] = useState<Record<string, string[]>>({});
  const [custom, setCustom] = useState<Record<string, string>>({});
  const [customOpen, setCustomOpen] = useState(false);
  const [active, setActive] = useState(0);
  // Option label currently hovered/focused; drives the detail preview row.
  const [hoverLabel, setHoverLabel] = useState<string | null>(null);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const customInputRef = useRef<HTMLInputElement | null>(null);
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
    setCustomOpen(false);
    setActive(0);
    setHoverLabel(null);
    if (advanceTimer.current != null) window.clearTimeout(advanceTimer.current);
    // The attention chime plays from the global runtime event stream (App.tsx),
    // keyed by prompt id — not here. A mount-time chime would double-fire for
    // the active tab and stay silent for background tabs.
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
  const showSubmitAction = q ? q.multi || customOpen || Boolean(custom[q.id]?.trim()) : false;

  useEffect(() => {
    setCustomOpen(false);
    setHoverLabel(null);
  }, [active]);

  useEffect(() => {
    if (customOpen) customInputRef.current?.focus();
  }, [customOpen]);

  const finishOrAdvance = (nextSel = sel, nextCustom = custom) => {
    if (advanceTimer.current != null) {
      window.clearTimeout(advanceTimer.current);
      advanceTimer.current = null;
    }
    if (isLast) {
      onAnswer(ask.id, answersFrom(nextSel, nextCustom));
      return;
    }
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
    setCustomOpen(false);

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
    setActive((i) => Math.max(0, i - 1));
  };

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;

      if (event.key === "Escape") {
        event.preventDefault();
        onStop();
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
  }, [active, custom, onDismiss, onStop, q, sel]);

  const answeredSummary = useMemo(
    () =>
      questions
        .slice(0, active)
        .map((question) => answerLabel(question))
        .filter(Boolean),
    [active, custom, questions, sel],
  );

  // Grid cells truncate descriptions to one line; this row previews the full
  // description of the hovered/focused option, falling back to the latest
  // selected option and then the first described option so it never blanks.
  const detailOption = useMemo(() => {
    if (!q) return null;
    const withDescription = (label?: string | null) =>
      label ? q.options.find((option) => option.label === label && option.description) : undefined;
    const hovered = withDescription(hoverLabel);
    if (hovered) return hovered;
    const selectedLabels = sel[q.id] ?? [];
    for (let i = selectedLabels.length - 1; i >= 0; i -= 1) {
      const chosen = withDescription(selectedLabels[i]);
      if (chosen) return chosen;
    }
    return q.options.find((option) => option.description) ?? null;
  }, [hoverLabel, q, sel]);

  if (!q) return null;

  return (
    <PromptShelf
      className="prompt-shelf--compact prompt-shelf--ask"
      barRef={shelfRef}
      titleId="ask-shelf-title"
      title={t("ask.title")}
      badges={
        <span className="ask-shelf__header-meta">
          {q.header && <span className="ask-shelf__header-text">{q.header}</span>}
          {hasMultipleQuestions && (
            <span className="ask-shelf__header-text ask-shelf__header-text--progress">
              {t("ask.questionProgress", { progress })}
            </span>
          )}
        </span>
      }
      meta={q.prompt}
      headerActions={
        <>
          <PromptHeaderAction onClick={onDismiss}>{t("ask.justChat")}</PromptHeaderAction>
          {!customOpen && (
            <PromptHeaderAction onClick={() => setCustomOpen(true)}>{t("ask.customAnswer")}</PromptHeaderAction>
          )}
          <PromptHeaderAction onClick={onStop} ariaLabel={t("composer.stopShort")}>Esc</PromptHeaderAction>
        </>
      }
      actions={
        <>
          {q.options.map((o, index) => {
            const on = (sel[q.id] ?? []).includes(o.label);
            return (
              <PromptAction
                key={o.label}
                keyLabel={q.options.length <= 9 ? String(index + 1) : ""}
                label={o.label}
                description={o.description}
                onClick={() => toggle(q, o.label)}
                selected={on}
                title={o.description || undefined}
                onHoverChange={(hovering) =>
                  setHoverLabel((current) => (hovering ? o.label : current === o.label ? null : current))
                }
              />
            );
          })}
        </>
      }
      note={
        detailOption && (
          <div className="ask-shelf__detail">
            <span className="ask-shelf__detail-label">{detailOption.label}</span>
            <span className="ask-shelf__detail-text">{detailOption.description}</span>
          </div>
        )
      }
      quickActions={
        <>
          {active > 0 && (
            <PromptAction keyLabel="" label={t("ask.back")} onClick={goBack} quiet />
          )}
          {showSubmitAction && (
            <PromptAction
              keyLabel=""
              label={isLast ? t("common.submit") : t("ask.next")}
              onClick={() => finishOrAdvance()}
              primary
              disabled={!currentAnswered}
            />
          )}
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
      {customOpen && (
      <div className="ask-shelf__custom-row">
        <input
          ref={customInputRef}
          className="ask-shelf__custom"
          placeholder={t("ask.customPlaceholder")}
          value={custom[q.id] ?? ""}
          onChange={(e) => setTyped(q, e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && currentAnswered) finishOrAdvance();
            e.stopPropagation();
          }}
        />
      </div>
      )}
    </PromptShelf>
  );
}
