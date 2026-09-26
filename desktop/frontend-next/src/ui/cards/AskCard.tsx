import type { AskReason } from "../../port/session";
import { Sym } from "../Sym";
import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import type { Item } from "../../state/session";
import { answerSource } from "../source";
import { useViewer } from "../../state/viewer";
import { useIme } from "../ime";

const ADVANCE_MS = 220;

interface Props {
  item: Extract<Item, { t: "ask" }>;
  onAnswer: (itemId: string, id: string, answers: { questionId: string; selected: string[] }[]) => Promise<void>;
}

export function AskCard({ item, onAnswer }: Props) {
  const qs = item.ask.questions;
  const answeredBy = answerSource(item.by, useViewer(), item.said);
  const [tab, setTab] = useState(0);
  const [picks, setPicks] = useState<string[][]>(() => qs.map(() => []));
  // The wire carries selections as free strings and the kernel joins them into
  // the tool result untouched, so "其他" is a label like any other.
  const [other, setOther] = useState<string[]>(() => qs.map(() => ""));
  const [otherOn, setOtherOn] = useState<boolean[]>(() => qs.map(() => false));
  const [submitting, setSubmitting] = useState(false);
  // Every request is a new object, so asking for the same pane twice still moves focus.
  const [focusTo, setFocusTo] = useState<{ q: number } | null>(null);
  const boxes = useRef<(HTMLInputElement | null)[]>([]);
  const panes = useRef<(HTMLDivElement | null)[]>([]);
  const ime = useIme();
  // Answered is read-only but still readable: the tabs keep working so you can
  // see what was chosen for each question, and the options stay on screen with
  // the unchosen ones dimmed by the sealed styling.
  const sealed = item.answered !== undefined;
  const chosen = item.answered ?? picks;

  const free = (i: number) => (otherOn[i] ? other[i].trim() : "");
  const selected = (i: number) => (free(i) ? [...picks[i], free(i)] : picks[i]);
  const answered = (i: number) => (sealed ? (chosen[i]?.length ?? 0) > 0 : selected(i).length > 0);
  const left = qs.reduce((n, _, i) => n + (answered(i) ? 0 : 1), 0);
  // A sealed card may have been answered by another client, so what counts as
  // free text is whatever came back that no option offered.
  const sealedFree = (i: number) => {
    const offered = new Set(qs[i].options.map((o) => o.label));
    return (chosen[i] ?? []).filter((v) => !offered.has(v)).join("、");
  };
  const freeShown = (i: number) => (sealed ? sealedFree(i) : other[i]);
  const customOption = (i: number) => qs[i].options.find((option) => isOtherOption(option.label));
  const customChosen = (i: number) => {
    const offered = customOption(i);
    return sealed
      ? !!sealedFree(i) || (!!offered && (chosen[i] ?? []).includes(offered.label))
      : otherOn[i];
  };

  const at = <T,>(list: T[], i: number, v: T) => list.map((x, k) => (k === i ? v : x));

  // The next question still waiting for an answer after `from`, wrapping, or -1.
  // `done` counts one more question as answered than state yet shows.
  const nextOpen = (from: number, done = -1) => {
    for (let k = 1; k < qs.length; k++) {
      const j = (from + k) % qs.length;
      if (j !== done && !answered(j)) return j;
    }
    return -1;
  };

  const toggle = (qi: number, label: string) => {
    if (sealed) return;
    setPicks((prev) => {
      if (!qs[qi].multi) return at(prev, qi, [label]);
      const has = prev[qi].includes(label);
      return at(prev, qi, has ? prev[qi].filter((l) => l !== label) : [...prev[qi], label]);
    });
    if (!qs[qi].multi) {
      setOtherOn((prev) => at(prev, qi, false));
      // One pick answers a single-choice question, so the card moves on by
      // itself; the pause lets the mark land before the pane changes.
      const next = nextOpen(qi, qi);
      if (next >= 0) window.setTimeout(() => setTab((cur) => (cur === qi ? next : cur)), ADVANCE_MS);
    }
  };

  const toggleOther = (qi: number) => {
    if (sealed) return;
    const on = !otherOn[qi];
    setOtherOn((prev) => at(prev, qi, on));
    if (on) setFocusTo({ q: qi });
    if (on && !qs[qi].multi) setPicks((prev) => at(prev, qi, []));
  };

  // A pane is display:none until it is the current one, so focus moves after
  // the render that shows it: into its open free-text box, else its first option.
  useEffect(() => {
    if (!focusTo) return;
    const box = boxes.current[focusTo.q];
    if (otherOn[focusTo.q] && box) box.focus();
    else panes.current[focusTo.q]?.querySelector<HTMLElement>("button.opt")?.focus();
  }, [focusTo]);

  const send = async (answers: string[][]) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onAnswer(item.id, item.ask.id, qs.map((q, i) => ({ questionId: q.id, selected: answers[i] })));
    } finally {
      setSubmitting(false);
    }
  };

  // Enter in the free-text box does what the primary button would, so a typed
  // answer never needs the mouse to go on.
  const onEnter = () => {
    if (submitting) return;
    if (nextOpen(tab, tab) >= 0) {
      if (!answered(tab)) return;
      const next = nextOpen(tab);
      setTab(next);
      setFocusTo({ q: next });
    } else if (left === 0) {
      void send(qs.map((_, i) => selected(i)));
    }
  };

  return (
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
      <div className="g">
        <Sym glyph="?" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{askHeading(qs)}</span>
          <span className="tag">{t("{n} 个问题", { n: qs.length })}</span>
        </div>
        <div className="out">
          <div className="ask" data-sealed={sealed ? "" : undefined} aria-busy={submitting}>
            {qs.length > 1 && (
              <div className="ask-tabs" role="tablist">
                {qs.map((q, i) => (
                  <button
                    key={q.id}
                    className="ask-tab"
                    role="tab"
                    aria-selected={i === tab}
                    data-answered={answered(i) ? "" : undefined}
                    onClick={() => setTab(i)}
                  >
                    {q.header || t("问题 {n}", { n: i + 1 })}
                    <i className="dot" />
                  </button>
                ))}
              </div>
            )}
            {qs.map((q, i) => (
              <div
                className="ask-pane"
                key={q.id}
                data-on={i === tab ? "" : undefined}
                ref={(el) => {
                  panes.current[i] = el;
                }}
              >
                <div className="ask-q">{q.prompt}</div>
                <div className="ask-hint">{t(q.multi ? "可多选" : "请选择一项")}</div>
                <div className="opts" aria-label={q.prompt}>
                  {q.options.filter((o) => !isOtherOption(o.label)).map((o) => (
                    <button
                      key={o.label}
                      className="opt"
                      data-multi={q.multi ? "" : undefined}
                      data-on={chosen[i]?.includes(o.label) ? "" : undefined}
                      aria-pressed={chosen[i]?.includes(o.label) ?? false}
                      disabled={sealed}
                      onClick={() => toggle(i, o.label)}
                    >
                      <span className="mark" />
                      <span className="txt">
                        <span className="lb">
                          {o.label}
                        </span>
                        {o.description && <span className="ds">{o.description}</span>}
                      </span>
                    </button>
                  ))}
                  <button
                    className="opt opt-other"
                    data-multi={q.multi ? "" : undefined}
                    data-on={customChosen(i) ? "" : undefined}
                    aria-pressed={customChosen(i)}
                    disabled={sealed}
                    onClick={() => toggleOther(i)}
                  >
                    <span className="mark" />
                    <span className="txt">
                      <span className="lb">{customOption(i)?.label ?? t("其他 —— 自行填写")}</span>
                      {customOption(i)?.description && <span className="ds">{customOption(i)?.description}</span>}
                    </span>
                  </button>
                </div>
                <div className="other-wrap" data-on={customChosen(i) ? "" : undefined}>
                  <input
                    ref={(el) => {
                      boxes.current[i] = el;
                    }}
                    data-action-keydown="ask.answer"
                    aria-label={customOption(i)?.label ?? t("其他 —— 自行填写")}
                    value={freeShown(i)}
                    readOnly={sealed}
                    placeholder={t("在此填写你希望采用的方案")}
                    onChange={(e) => setOther((prev) => at(prev, i, e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key !== "Enter" || sealed || ime.isIme(e.nativeEvent)) return;
                      e.preventDefault();
                      onEnter();
                    }}
                    {...ime.handlers}
                  />
                </div>
              </div>
            ))}
            {sealed && (
              <div className="ask-done">
                {item.answeredElsewhere ? (
                  <b>{answeredBy || t("已在其他窗口处理，请以最新运行状态为准。")}</b>
                ) : (
                  <>
                    {qs.map((q, i) => (
                      <span key={q.id}>
                        {i > 0 && "　·　"}
                        <b>{q.header || t("问题 {n}", { n: i + 1 })}：</b>
                        {chosen[i]?.length ? chosen[i].join("、") : t("未答")}
                      </span>
                    ))}
                  </>
                )}
              </div>
            )}
            {!sealed && (
              <div className="ask-foot">
                {/* An answer batch with nothing selected is the kernel's explicit
                    "don't decide for me" path: it ends the turn rather than
                    feeding a prose dismissal back to the model. */}
                <button className="dismiss" data-action="ask.answer" data-value="none" disabled={submitting} onClick={() => void send(qs.map(() => []))}>
                  {t("先不选择，直接回复")}
                </button>
                {qs.length > 1 && tab > 0 && (
                  <button className="dismiss" data-action="ask.step" data-value="prev" disabled={submitting} onClick={() => setTab(tab - 1)}>
                    {t("上一题")}
                  </button>
                )}
                {nextOpen(tab, tab) >= 0 ? (
                  // While another question is still open the primary step is to
                  // it; it waits for this one to be answered first.
                  <button className="btn" data-primary data-action="ask.step" data-value="next"
                    disabled={!answered(tab) || submitting} onClick={() => setTab(nextOpen(tab))}>
                    {t("下一题（{i}/{n}）", { i: tab + 1, n: qs.length })}
                  </button>
                ) : (
                  <button className="btn" data-primary data-action="ask.answer" data-value="chosen" disabled={left > 0 || submitting} onClick={() => void send(qs.map((_, i) => selected(i)))}>
                    {submitting ? t("正在提交…") : t("确认")}
                  </button>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function isOtherOption(label: string): boolean {
  return /^\s*(?:其他|其它|other)(?:\s|[（(—\-:：]|$)/i.test(label);
}

// reasonLabel is exhaustive on purpose. A third reason would be a decision with
// a different owner — permission and plan approval are answered elsewhere — and
// adding one must fail this compile rather than quietly render as a question.
function reasonLabel(reason: AskReason): string {
  switch (reason) {
    case "user_decision":
      return "需要你决定";
    case "missing_value":
      return "需要你补充信息";
    default: {
      const unhandled: never = reason;
      return unhandled;
    }
  }
}

// A batch may mix the two; naming both is more honest than picking one.
function askHeading(qs: { reason?: AskReason }[]): string {
  const kinds = new Set<AskReason>(qs.map((q) => q.reason ?? "user_decision"));
  return [...kinds].map(reasonLabel).join(" · ");
}
