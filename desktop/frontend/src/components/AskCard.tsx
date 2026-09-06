import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, X } from "lucide-react";
import type { QuestionAnswer, WireAsk, WireAskQuestion } from "../lib/types";
import {
  DecisionConfirmBar,
  PromptAction,
  PromptDescriptionDisclosure,
  PromptHeaderAction,
  PromptShelf,
} from "./PromptShelf";

const askDrafts = new Map<string, AskDraft>();
const askDraftKey = (scope: string, ask: WireAsk) =>
  JSON.stringify([scope, ask.runtimeEpoch ?? "", ask.turnId ?? "", ask.id]);
type AnswerMode = "option" | "custom";
type AskDraft = {
  sel: Record<string, string[]>;
  custom: Record<string, string>;
  answerMode: Record<string, AnswerMode>;
  active: number;
  selectedIndex: number;
};

function readAskDraft(key: string): AskDraft | undefined {
  const draft = askDrafts.get(key);
  return draft ? { ...draft, sel: { ...draft.sel }, custom: { ...draft.custom }, answerMode: { ...draft.answerMode } } : undefined;
}

function clearAskDraft(key: string): void {
  askDrafts.delete(key);
}

// AskCard renders the `ask` tool as a decision shelf near the composer. It
// walks multi-question asks one at a time. Single-select choices advance to
// the next question immediately; multi-select and custom answers wait for an
// explicit confirm, and the final question still requires submission.
type AskCardProps = {
  ask: WireAsk;
  onAnswer: (id: string, answers: QuestionAnswer[]) => void | Promise<void>;
  onDismiss?: () => void | Promise<void>;
  draftScope?: string;
  onStop: () => void;
};

export function AskCard(props: AskCardProps) {
  const draftKey = askDraftKey(props.draftScope ?? "default", props.ask);
  // The same identity owns both cached drafts and live component state. A
  // controller can reuse prompt IDs, so changing runtime or turn remounts it.
  return <AskCardBody key={draftKey} {...props} draftKey={draftKey} />;
}

function AskCardBody({ ask, onAnswer, onStop, draftKey }: AskCardProps & { draftKey: string }) {
  const t = useT();
  // Per-question state: selected option labels, and an optional typed answer.
  const [sel, setSel] = useState<Record<string, string[]>>(() => readAskDraft(draftKey)?.sel ?? {});
  const [custom, setCustom] = useState<Record<string, string>>(() => readAskDraft(draftKey)?.custom ?? {});
  const [answerMode, setAnswerMode] = useState<Record<string, AnswerMode>>(() => readAskDraft(draftKey)?.answerMode ?? {});
  const [customOpen, setCustomOpen] = useState(false);
  const [active, setActive] = useState(() => readAskDraft(draftKey)?.active ?? 0);
  // Extra decision row after option labels: custom answer. Skip is a
  // secondary footer action rather than an answer choice.
  const [selectedIndex, setSelectedIndex] = useState(() => readAskDraft(draftKey)?.selectedIndex ?? 0);
  const [expandedDescriptionId, setExpandedDescriptionId] = useState<string | null>(null);
  const [descriptionTruncated, setDescriptionTruncated] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  // A newly delivered Ask always starts expanded, matching harness. Collapse
  // is presentation state and must not leak from an earlier request.
  const [collapsed, setCollapsed] = useState(false);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const customInputRef = useRef<HTMLInputElement | null>(null);
  const initializedActiveRef = useRef(false);
  const instanceId = useId();

  const questions = ask.questions;
  const q = questions[Math.min(active, questions.length - 1)];
  const isLast = active >= questions.length - 1;
  const progress = `${Math.min(active + 1, questions.length)}/${questions.length}`;
  const hasMultipleQuestions = questions.length > 1;

  // Row layout: [options...] [custom]
  const optionCount = q?.options.length ?? 0;
  const customRowIndex = optionCount;
  const rowCount = optionCount + 1;
  const selectedOption = selectedIndex >= 0 && selectedIndex < optionCount
    ? q?.options[selectedIndex]
    : undefined;
  const selectedDescriptionId = selectedOption
    ? `${instanceId}-description-${selectedIndex}`
    : undefined;
  const descriptionExpanded = selectedDescriptionId !== undefined && expandedDescriptionId === selectedDescriptionId;

  useEffect(() => {
    shelfRef.current?.focus();
  }, []);

  useEffect(() => {
    try {
      askDrafts.set(draftKey, { sel: { ...sel }, custom: { ...custom }, answerMode: { ...answerMode }, active, selectedIndex });
    } catch {
      // Draft storage is best effort; the live pending ask remains authoritative.
    }
  }, [active, answerMode, custom, draftKey, selectedIndex, sel]);

  useEffect(() => {
    setCustomOpen(false);
    if (initializedActiveRef.current) setSelectedIndex(0);
    initializedActiveRef.current = true;
  }, [active]);

  useEffect(() => {
    setExpandedDescriptionId(null);
  }, [active, ask.id]);

  useEffect(() => {
    if (customOpen) customInputRef.current?.focus();
  }, [customOpen]);

  const answersFrom = (
    nextSel: Record<string, string[]> = sel,
    nextCustom: Record<string, string> = custom,
  ): QuestionAnswer[] =>
    questions.map((question) => ({
      questionId: question.id,
      selected: answerMode[question.id] === "custom" && nextCustom[question.id]?.trim()
        ? [nextCustom[question.id].trim()]
        : (nextSel[question.id] ?? []),
    }));

  const answerLabel = (question: WireAskQuestion) => {
    if (answerMode[question.id] === "custom") return custom[question.id]?.trim() ?? "";
    return (sel[question.id] ?? []).join(", ");
  };

  const answered = (question: WireAskQuestion) => answerMode[question.id] === "custom"
    ? (custom[question.id]?.trim() ?? "") !== ""
    : (sel[question.id]?.length ?? 0) > 0;

  const currentAnswered = q ? answered(q) : false;

  const submitAction = (action: () => void | Promise<void>) => {
    if (submitting) return;
    setSubmitting(true);
    void Promise.resolve()
      .then(action)
      .catch(() => setSubmitting(false));
  };

  const finishOrAdvance = (nextSel = sel, nextCustom = custom) => {
    if (submitting) return;
    if (isLast) {
      submitAction(() => Promise.resolve(onAnswer(ask.id, answersFrom(nextSel, nextCustom))).then(() => {
        clearAskDraft(draftKey);
      }));
      return;
    }
    setActive((i) => Math.min(i + 1, questions.length - 1));
  };

  const toggleOption = (question: WireAskQuestion, label: string) => {
    if (submitting) return;
    const cur = sel[question.id] ?? [];
    const nextSel = question.multi
      ? { ...sel, [question.id]: cur.includes(label) ? cur.filter((x) => x !== label) : [...cur, label] }
      : { ...sel, [question.id]: [label] };

    setAnswerMode((m) => ({ ...m, [question.id]: "option" }));
    setSel(nextSel);
    setCustomOpen(false);
  };

  const setTyped = (question: WireAskQuestion, text: string) => {
    setSelectedIndex(customRowIndex);
    setAnswerMode((m) => ({ ...m, [question.id]: "custom" }));
    setCustom((c) => ({ ...c, [question.id]: text }));
    if (text.trim()) setSel((s) => ({ ...s, [question.id]: [] }));
  };

  const goBack = () => {
    if (submitting) return;
    setActive((i) => Math.max(0, i - 1));
  };

  const stopAsk = () => {
    clearAskDraft(draftKey);
    onStop();
  };

  const skipCurrentQuestion = () => {
    if (submitting || !q) return;
    const nextSel = { ...sel, [q.id]: [] };
    const nextCustom = { ...custom, [q.id]: "" };
    setSel(nextSel);
    setCustom(nextCustom);
    setCustomOpen(false);
    if (!isLast) {
      setActive((i) => i + 1);
      return;
    }
    submitAction(() => Promise.resolve(onAnswer(ask.id, answersFrom(nextSel, nextCustom))).then(() => {
      clearAskDraft(draftKey);
    }));
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
        // Single-select follows harness behavior: choose and advance, while
        // keeping the answer in the draft so Back can revise it.
        setAnswerMode((m) => ({ ...m, [q.id]: "option" }));
        setSel((s) => ({ ...s, [q.id]: [option.label] }));
        setCustomOpen(false);
        if (active < questions.length - 1) setActive((i) => i + 1);
      }
    } else if (index === customRowIndex) {
      // Opening custom clears option picks for this question.
      setAnswerMode((m) => ({ ...m, [q.id]: "custom" }));
      setCustomOpen(true);
      setSel((s) => ({ ...s, [q.id]: [] }));
    }
  };

  const canConfirm = (): boolean => {
    if (!q || submitting) return false;
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
        stopAsk();
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

  const confirmLabel = isLast
      ? t("common.submit")
      : t("ask.next");

  return (
    <PromptShelf
      decision
      className="prompt-shelf--ask"
      cardCollapsible
      collapsed={collapsed}
      onToggleCollapse={() => setCollapsed((value) => !value)}
      barRef={shelfRef}
      titleId="ask-shelf-title"
      title={q.header ?? t("ask.title")}
      badges={
        <span className="ask-shelf__header-meta">
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
          <PromptHeaderAction
            onClick={() => setCollapsed((value) => !value)}
            ariaLabel={collapsed ? t("common.expand") : t("common.collapse")}
            disabled={submitting}
          >
            {collapsed ? <ChevronUp size={15} aria-hidden="true" /> : <ChevronDown size={15} aria-hidden="true" />}
          </PromptHeaderAction>
          <PromptHeaderAction onClick={stopAsk} ariaLabel={t("decision.stopTask")} disabled={submitting}>
            <X size={16} aria-hidden="true" />
          </PromptHeaderAction>
        </>
      }
      actions={
        <>
          {q.options.map((o, index) => {
            const on = (sel[q.id] ?? []).includes(o.label);
            const cursor = selectedIndex === index;
            return (
              <PromptAction
                key={o.label}
                actionId={`${instanceId}-row-${index}`}
                keyLabel={q.options.length <= 9 ? String(index + 1) : ""}
                label={o.label}
                description={o.description}
                descriptionId={`${instanceId}-description-${index}`}
                descriptionDisclosure
                onDescriptionOverflowChange={selectedIndex === index ? setDescriptionTruncated : undefined}
                onClick={() => selectRow(index)}
                // Single-select: cursor owns selection. Multi-select: selected
                // means checked; active is the keyboard cursor only.
                selected={q.multi ? on : cursor}
                active={q.multi ? cursor : false}
                disabled={submitting}
              />
            );
          })}
          <div
            className={`ask-shelf__custom-row${custom[q.id]?.trim() ? " ask-shelf__custom-row--active" : ""}`}
            role="group"
            onClick={() => {
              setSelectedIndex(customRowIndex);
              setAnswerMode((m) => ({ ...m, [q.id]: "custom" }));
              setCustomOpen(true);
              customInputRef.current?.focus();
            }}
          >
            <span className="ask-shelf__custom-indicator" aria-hidden="true">✎</span>
            <input
              ref={customInputRef}
              className="ask-shelf__custom"
              aria-label={t("ask.customAnswer")}
              placeholder={t("ask.customPlaceholder")}
              value={answerMode[q.id] === "custom" ? custom[q.id] ?? "" : ""}
              disabled={submitting}
              onFocus={() => setSelectedIndex(customRowIndex)}
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
      note={
        <>
          {selectedDescriptionId && descriptionTruncated && (
            <PromptDescriptionDisclosure
              descriptionId={`${selectedDescriptionId}-detail`}
              label={selectedOption?.label}
              description={selectedOption?.description}
              expanded={descriptionExpanded}
              onToggle={() => setExpandedDescriptionId((current) => current === selectedDescriptionId ? null : selectedDescriptionId)}
              disabled={submitting}
            />
          )}
        </>
      }
      footer={
        <div className="ask-shelf__footer-layout">
          <div className="ask-shelf__pager" aria-label={t("ask.questionProgress", { progress })}>
            <button type="button" className="ask-shelf__pager-button" aria-label={t("ask.back")} disabled={active === 0 || submitting} onClick={goBack}>
              <ChevronLeft size={16} aria-hidden="true" />
            </button>
            <span>{progress}</span>
            <button type="button" className="ask-shelf__pager-button" aria-label={t("ask.next")} disabled={isLast || submitting} onClick={() => setActive((i) => Math.min(i + 1, questions.length - 1))}>
              <ChevronRight size={16} aria-hidden="true" />
            </button>
          </div>
          <DecisionConfirmBar
            hint={t("decision.selectHint")}
            confirmLabel={confirmLabel}
            onConfirm={confirmSelected}
            secondaryLabel={t("ask.skipQuestion")}
            onSecondary={skipCurrentQuestion}
            disabled={submitting}
            confirmDisabled={!canConfirm()}
          />
        </div>
      }
    />
  );
}
