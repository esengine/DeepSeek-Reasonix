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
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("MCP 服务器 {name} 请求信息", { name: origin?.source ?? "" })}</span>
          <span className="tag">{t("{n} 项", { n: qs.length })}</span>
        </div>
        <div className="out">
          <div className="ask" data-sealed={sealed ? "" : undefined} aria-busy={submitting}>
            <div className="ask-pane" data-on="">
              <div className="ask-hint">{t("这是外部服务器的请求，不是 agent 的提问。请勿填写密码、密钥等敏感信息。")}</div>
              {origin?.message && <div className="ask-q">{origin.message}</div>}
              {origin?.note && !sealed && <div className="ask-hint">{t("上次填写未通过：{note}", { note: origin.note })}</div>}
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
                      placeholder={t("填写这一项")}
                      onChange={(e) => setText((prev) => at(prev, i, e.target.value))}
                    />
                  </div>
                )}
              </div>
            ))}
            {sealed ? (
              <div className="ask-done">
                {item.answeredElsewhere ? (
                  <b>{answeredBy || t("已在其他窗口处理，请以最新运行状态为准。")}</b>
                ) : item.answered?.some((a) => a.length) ? (
                  <b>{t("已提交给 {name}", { name: origin?.source ?? "" })}</b>
                ) : (
                  <b>{t("已拒绝提供")}</b>
                )}
              </div>
            ) : (
              <div className="ask-foot">
                <button className="dismiss" data-action="ask.answer" data-value="none" disabled={submitting} onClick={() => void send(qs.map(() => []))}>
                  {t("拒绝提供")}
                </button>
                <button className="btn" data-primary data-action="ask.answer" data-value="chosen" disabled={!any || submitting}
                  onClick={() => void send(qs.map((_, i) => given(i)))}>
                  {submitting ? t("正在提交…") : t("提交")}
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
