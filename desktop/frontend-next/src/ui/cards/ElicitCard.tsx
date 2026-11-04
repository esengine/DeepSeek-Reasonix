import { useState } from "react";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import type { Item } from "../../state/session";
import { answerSource } from "../source";
import { useViewer } from "../../state/viewer";
import { ElicitURLCard } from "./ElicitURLCard";

export interface ElicitProps {
  item: Extract<Item, { t: "ask" }>;
  onAnswer: (itemId: string, id: string, answers: { questionId: string; selected: string[] }[]) => Promise<void>;
}

export function ElicitCard(props: ElicitProps) {
  return props.item.ask.origin?.url ? <ElicitURLCard {...props} /> : <ElicitFormCard {...props} />;
}

function ElicitFormCard({ item, onAnswer }: ElicitProps) {
  const qs = item.ask.questions;
  const origin = item.ask.origin;
  const answeredBy = answerSource(item.by, useViewer(), item.said);
  const [picks, setPicks] = useState<string[][]>(() => qs.map((q) => (q.options.length ? q.default ?? [] : [])));
  const [text, setText] = useState<string[]>(() => qs.map((q) => (q.options.length ? "" : q.default?.[0] ?? "")));
  const [submitting, setSubmitting] = useState(false);
  const sealed = item.answered !== undefined;

  const given = (i: number) => (qs[i].options.length ? picks[i] : text[i].trim() ? [text[i].trim()] : []);
  const any = qs.some((_, i) => given(i).length > 0);
  const at = <T,>(list: T[], i: number, v: T) => list.map((x, k) => (k === i ? v : x));

  const toggle = (i: number, label: string) => {
    if (sealed) return;
    setPicks((prev) => {
      if (!qs[i].multi) return at(prev, i, prev[i][0] === label ? [] : [label]);
      return at(prev, i, prev[i].includes(label) ? prev[i].filter((l) => l !== label) : [...prev[i], label]);
    });
  };

  const send = async (answers: string[][]) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onAnswer(item.id, item.ask.id, qs.map((q, i) => ({ questionId: q.id, selected: answers[i] })));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
      <div className="g">
        <Sym glyph="?" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("chat.mcp_server_name_is_asking", { name: origin?.source ?? "" })}</span>
          <span className="tag" data-pending={sealed ? undefined : ""}>{sealed ? t("chat.handled") : t("chat.needs_you")}</span>
        </div>
        <div className="out">
          <div className="ask" data-sealed={sealed ? "" : undefined} aria-busy={submitting}>
            <div className="ask-pane" data-on="">
              <div className="ask-hint">{t("chat.this_request_comes_from_an")}</div>
              {origin?.message && <div className="ask-q">{origin.message}</div>}
              {origin?.note && !sealed && <div className="ask-hint">{t("chat.the_last_answer_was_not", { note: origin.note })}</div>}
            </div>
            {qs.map((q, i) => (
              <div className="ask-pane" data-on="" key={q.id}>
                <div className="ask-q">{q.header || q.id}</div>
                {q.prompt && q.prompt !== q.header && <div className="ask-hint">{q.prompt}</div>}
                {q.options.length > 0 ? (
                  <div className="opts" aria-label={q.header || q.id}>
                    {q.options.map((o) => {
                      const on = (sealed ? item.answered?.[i] : picks[i])?.includes(o.label) ?? false;
                      return (
                        <button key={o.label} className="opt" data-multi={q.multi ? "" : undefined} data-on={on ? "" : undefined}
                          aria-pressed={on} disabled={sealed} onClick={() => toggle(i, o.label)}>
                          <span className="mark" />
                          <span className="txt"><span className="lb">{o.label}</span></span>
                        </button>
                      );
                    })}
                  </div>
                ) : (
                  <div className="other-wrap" data-on="">
                    <input
                      aria-label={q.header || q.id}
                      value={sealed ? (item.answered?.[i] ?? []).join("") : text[i]}
                      readOnly={sealed}
                      placeholder={t("chat.fill_this_in")}
                      onChange={(e) => setText((prev) => at(prev, i, e.target.value))}
                    />
                  </div>
                )}
              </div>
            ))}
            {sealed ? (
              <div className="ask-done">
                {item.answeredElsewhere ? (
                  <b>{answeredBy || t("chat.handled_in_another_window_refer")}</b>
                ) : item.answered?.some((a) => a.length) ? (
                  <b>{t("chat.sent_to_name", { name: origin?.source ?? "" })}</b>
                ) : (
                  <b>{t("chat.declined")}</b>
                )}
              </div>
            ) : (
              <div className="ask-foot">
                <button className="dismiss" data-action="ask.answer" data-value="none" disabled={submitting} onClick={() => void send(qs.map(() => []))}>
                  {t("chat.decline")}
                </button>
                <button className="btn" data-primary data-action="ask.answer" data-value="chosen" disabled={!any || submitting}
                  onClick={() => void send(qs.map((_, i) => given(i)))}>
                  {submitting ? t("common.submitting") : t("chat.submit")}
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
