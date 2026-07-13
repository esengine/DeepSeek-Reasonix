import { useEffect, useMemo, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { QuestionAnswer, WireAsk, WireAskQuestion } from "../lib/types";
import {
  DecisionConfirmBar,
  PromptAction,
  PromptHeaderAction,
  PromptShelf,
} from "./PromptShelf";

// AskCard renders the `ask` tool as a decision shelf near the composer. It
// walks multi-question asks one at a time. Selecting (click / digit) never
// advances; Enter / Confirm submits or moves to the next question.
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
  // Extra decision rows after option labels: custom answer / skip chat.
  // selectedIndex indexes options, then custom, then skip.
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const customInputRef = useRef<HTMLInputElement | null>(null);

  const questions = ask.questions;
  const q = questions[Math.min(active, questions.length - 1)];
  const isLast = active >= questions.length - 1;
  const progress = `${Math.min(active + 1, questions.length)}/${questions.length}`;
  const hasMultipleQuestions = questions.length > 1;

  // Row layout: [options...] [custom] [skip]
  const optionCount = q?.options.length ?? 0;
  const customRowIndex = optionCount;
  const skipRowIndex = optionCount + 1;
  const rowCount = optionCount + 2;

  useEffect(() => {
    shelfRef.current?.focus();
    setSel({});
    setCustom({});
    setCustomOpen(false);
    setActive(0);
    setSelectedIndex(0);
    setSubmitting(false);
  }, [ask.id]);

  useEffect(() => {
    setCustomOpen(false);
    setSelectedIndex(0);
  }, [active]);

  useEffect(() => {
    if (customOpen) customInputRef.current?.focus();
  }, [customOpen]);

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
    if (submitting) return;
    if (isLast) {
      setSubmitting(true);
      onAnswer(ask.id, answersFrom(nextSel, nextCustom));
      return;
    }
    setActive((i) => Math.min(i + 1, questions.length - 1));
  };

  const toggleOption = (question: WireAskQuestion, label: string) => {
    if (submitting) return;
    const nextCustom = { ...custom, [question.id]: "" };
    const cur = sel[question.id] ?? [];
    const nextSel = question.multi
      ? { ...sel, [question.id]: cur.includes(label) ? cur.filter((x) => x !== label) : [...cur, label] }
      : { ...sel, [question.id]: [label] };

    setCustom(nextCustom);
    setSel(nextSel);
    setCustomOpen(false);
  };

  const setTyped = (question: WireAskQuestion, text: string) => {
    setCustom((c) => ({ ...c, [question.id]: text }));
    if (text.trim()) setSel((s) => ({ ...s, [question.id]: [] }));
  };

  const goBack = () => {
    if (submitting) return;
    setActive((i) => Math.max(0, i - 1));
  };

  const selectRow = (index: number) => {
    if (submitting || !q) return;
    setSelectedIndex(index);
    if (index < optionCount) {
      const option = q.options[index];
      if (!option) return;
      if (q.multi) {
        toggleOption(q, option.label);
      } else {
        // Single-select: click/digit only selects the row and marks the option.
        setCustom((c) => ({ ...c, [q.id]: "" }));
        setSel((s) => ({ ...s, [q.id]: [option.label] }));
        setCustomOpen(false);
      }
    } else if (index === customRowIndex) {
      // Opening custom clears option picks for this question.
      setCustomOpen(true);
      setSel((s) => ({ ...s, [q.id]: [] }));
    } else if (index === skipRowIndex) {
      setCustomOpen(false);
      setCustom((c) => ({ ...c, [q.id]: "" }));
      setSel((s) => ({ ...s, [q.id]: [] }));
    }
  };

  const canConfirm = (): boolean => {
    if (!q || submitting) return false;
    if (selectedIndex === skipRowIndex) return true;
    if (selectedIndex === customRowIndex) {
      return Boolean(custom[q.id]?.trim());
    }
    // Multi-select: answers come from checked options / typed custom, not the
    // keyboard cursor alone.
    if (q.multi) return currentAnswered;
    // Single-select: the keyboard cursor is authoritative for option rows so
    // initial Enter and ArrowDown+Enter work without a prior click.
    if (selectedIndex >= 0 && selectedIndex < optionCount) return true;
    return (sel[q.id]?.length ?? 0) > 0;
  };

  const confirmSelected = () => {
    if (!q || submitting || !canConfirm()) return;
    if (selectedIndex === skipRowIndex) {
      setSubmitting(true);
      // Skip and continue chat: submit empty answers for the whole ask.
      onDismiss();
      return;
    }
    if (selectedIndex === customRowIndex) {
      finishOrAdvance();
      return;
    }
    // Ensure the highlighted option is reflected for single-select.
    if (!q.multi && selectedIndex < optionCount) {
      const option = q.options[selectedIndex];
      if (option) {
        const nextSel = { ...sel, [q.id]: [option.label] };
        const nextCustom = { ...custom, [q.id]: "" };
        setSel(nextSel);
        setCustom(nextCustom);
        finishOrAdvance(nextSel, nextCustom);
        return;
      }
    }
    finishOrAdvance();
  };

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (submitting || !q) return;
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || (target instanceof HTMLElement && target.isContentEditable)) return;

      if (event.key === "Escape") {
        event.preventDefault();
        onStop();
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((i) => (i - 1 + rowCount) % rowCount);
        return;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((i) => (i + 1) % rowCount);
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        confirmSelected();
        return;
      }
      if ((event.key === "ArrowLeft" || event.key === "Backspace") && active > 0) {
        event.preventDefault();
        goBack();
        return;
      }

      const index = Number(event.key) - 1;
      if (!Number.isInteger(index) || index < 0 || index >= optionCount) return;
      event.preventDefault();
      selectRow(index);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  });

  const answeredSummary = useMemo(
    () =>
      questions
        .slice(0, active)
        .map((question) => answerLabel(question))
        .filter(Boolean),
    [active, custom, questions, sel],
  );

  if (!q) return null;

  const confirmLabel = selectedIndex === skipRowIndex
    ? t("ask.justChat")
    : isLast
      ? t("common.submit")
      : t("ask.next");

  return (
    <PromptShelf
      decision
      className="prompt-shelf--ask"
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
        <PromptHeaderAction onClick={onStop} ariaLabel={t("decision.stopTask")} disabled={submitting}>
          {t("decision.stopTask")}
        </PromptHeaderAction>
      }
      actions={
        <>
          {q.options.map((o, index) => {
            const on = (sel[q.id] ?? []).includes(o.label);
            const cursor = selectedIndex === index;
            return (
              <PromptAction
                key={o.label}
                keyLabel={q.options.length <= 9 ? String(index + 1) : ""}
                label={o.label}
                description={o.description}
                onClick={() => selectRow(index)}
                // Single-select: cursor owns selection. Multi-select: selected
                // means checked; active is the keyboard cursor only.
                selected={q.multi ? on : cursor}
                active={q.multi ? cursor : false}
                disabled={submitting}
                title={o.description || undefined}
              />
            );
          })}
          <PromptAction
            keyLabel=""
            label={t("ask.customAnswer")}
            description={t("ask.customAnswerDesc")}
            onClick={() => selectRow(customRowIndex)}
            selected={selectedIndex === customRowIndex || customOpen}
            disabled={submitting}
          />
          <PromptAction
            keyLabel=""
            label={t("ask.justChat")}
            description={t("ask.justChatDesc")}
            onClick={() => selectRow(skipRowIndex)}
            selected={selectedIndex === skipRowIndex}
            disabled={submitting}
            tone="danger"
          />
        </>
      }
      quickActions={
        active > 0 ? (
          <PromptAction keyLabel="" label={t("ask.back")} onClick={goBack} quiet disabled={submitting} role="button" />
        ) : undefined
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
      footer={
        <DecisionConfirmBar
          hint={t("decision.selectHint")}
          confirmLabel={confirmLabel}
          onConfirm={confirmSelected}
          disabled={submitting}
          confirmDisabled={!canConfirm()}
        />
      }
    >
      {customOpen && (
        <div className="ask-shelf__custom-row">
          <input
            ref={customInputRef}
            className="ask-shelf__custom"
            placeholder={t("ask.customPlaceholder")}
            value={custom[q.id] ?? ""}
            disabled={submitting}
            onChange={(e) => setTyped(q, e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && canConfirm()) {
                e.preventDefault();
                confirmSelected();
              }
              e.stopPropagation();
            }}
          />
        </div>
      )}
    </PromptShelf>
  );
}
