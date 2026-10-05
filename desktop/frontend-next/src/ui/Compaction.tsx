import { useEffect, useId, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, CompactionSettings } from "../port/port";
import { foldModeOf, foldModeValue, type FoldMode as Mode } from "./foldbound";

const tokens = (n: number) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

// Capacity is the default. A fixed token threshold is an explicit override and
// stays visible because hiding the policy made it hard to discover and audit.
export function Compaction({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [box, setBox] = useState<CompactionSettings | null>(null);
  const [used, setUsed] = useState<number | null>(null);
  const [draft, setDraft] = useState("");
  // What the reader has asked for, which leads the stored value: "custom" is a
  // choice before it is a number, and deriving the mode from storage alone left
  // that click with nothing to show — the field appears only once a value has
  // been committed, so it could never be committed.
  const [choice, setChoice] = useState<Mode | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const field = useId();

  useEffect(() => {
    port
      .compaction()
      .then((s) => {
        setBox(s);
        setDraft(String(s.soft_limit_tokens > 0 ? s.soft_limit_tokens : Math.round(s.context_window * s.ratio)));
        setChoice(null);
      })
      .catch(() => setBox(null));
    // The threshold is a number until it is next to the session's own usage,
    // and then it is a distance. A gauge that fails to load costs the reader
    // the distance, not the setting.
    port.context().then((c) => setUsed(c.used)).catch(() => setUsed(null));
  }, [port]);

  const sent = useRef<number | null>(null);

  if (!box) return <div className="empty">{t("无法读取压缩配置。")}</div>;

  const save = async (value: number) => {
    setBusy(true);
    setError("");
    try {
      const saved = await port.saveCompaction(value);
      setBox(saved);
      setDraft(String(saved.soft_limit_tokens > 0 ? saved.soft_limit_tokens : Math.round(saved.context_window * saved.ratio)));
      setChoice(null);
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  // Declaring the bound rebuilds the runtime, so one gesture must reach the
  // kernel once: Enter commits and then blurs, which runs a text field's commit
  // twice for a single keystroke.
  const send = (value: number) => {
    if (value === sent.current || value === box.soft_limit_tokens) return;
    sent.current = value;
    void save(value);
  };

  const mode = choice ?? foldModeOf(box.soft_limit_tokens);
  const commitCustom = () => {
    const text = draft.trim();
    if (text === "") return;
    const next = Number(text);
    if (!Number.isFinite(next) || !Number.isInteger(next) || next < 1000 || (!off && next >= win)) {
      setError(t("阈值需至少为 1,000，并小于模型上下文窗口。"));
      return;
    }
    setError("");
    send(next);
  };

  const pick = (next: Mode) => {
    setError("");
    setChoice(next);
    const value = foldModeValue[next];
    if (value !== undefined) return send(value);
    // Custom is not a value on its own — it opens the field on whatever is
    // already there, and the write happens when a number is committed.
  };

  const win = box.context_window;
  const capacity = Math.round(win * box.ratio);
  const off = win === 0;
  const economicWins = !off && mode !== "capacity" && box.trigger < capacity;
  const pct = off || !used ? 0 : Math.min((used / box.trigger) * 100, 100);

  return (
    <>
      <div className="cmp">
        {off ? (
          <p className="note">{t("该来源未声明上下文窗口，因此不会自动整理上下文。请先在右侧「上下文」中填写该模型的窗口大小。")}</p>
        ) : (
          <>
            <div className="cmpg">
              <div className="r">
                <span className="k">{t("当前")}</span>
                <b>{used === null ? "—" : tokens(used)}</b>
              </div>
              <div className="r">
                <span className="k">{t("下次整理")}</span>
                <b>{tokens(box.trigger)}</b>
              </div>
              <div className="r">
                <span className="k">{t("模型窗口")}</span>
                <b>{tokens(win)}</b>
              </div>
            </div>
            {used !== null && (
              <div className="cmpbar" role="presentation">
                <i style={{ width: `${pct}%` }} />
              </div>
            )}
            <p className="note">
              {economicWins
                ? t("经济维护阈值会先到，因此本会话预计在 {n} tokens 左右自动整理上下文。", { n: tokens(box.trigger) })
                : t("模型窗口的容量保护会先到，因此本会话预计在 {n} tokens 左右自动整理上下文。", { n: tokens(box.trigger) })}
            </p>
          </>
        )}
      </div>

      <div className="lrow">
            <span className="tx">
              <span className="lb">{t("整理策略")}</span>
              <span className="ds">
                {t("默认跟随模型容量；需要提前整理时再设置固定阈值。")}
              </span>
            </span>
            <div className="seg" data-text role="group" aria-label={t("整理策略")}>
              {([
                ["capacity", t("按模型容量（默认）")],
                ["custom", t("自定义")],
              ] as [Mode, string][]).map(([m, label]) => (
                <button
                  key={m}
                  data-action="compaction.threshold"
                  data-value={m}
                  aria-pressed={mode === m}
                  disabled={busy}
                  onClick={() => pick(m)}
                >
                  {label}
                </button>
              ))}
            </div>
      </div>

          {mode === "custom" && (
            <div className="lrow threshold-row">
              <span className="tx">
                <label className="lb" htmlFor={field}>{t("阈值")}</label>
                <span className="ds">{t("达到这个用量时开始整理")}</span>
              </span>
              <div className="threshold-control">
                <span className="unit-field">
                  <input
                    id={field}
                    type="text"
                    inputMode="numeric"
                    value={draft}
                    disabled={busy}
                    placeholder={String(capacity)}
                    onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
                    onBlur={commitCustom}
                    data-action-keydown="compaction.threshold"
                    onKeyDown={(e) => {
                      if (e.key !== "Enter") return;
                      commitCustom();
                      e.currentTarget.blur();
                    }}
                  />
                  <i>tokens</i>
                </span>
                <span className="threshold-presets" aria-label={t("常用阈值")}>
                  {[0.5, 0.7, 0.85].map((ratio) => {
                    const value = Math.round(win * ratio / 1000) * 1000;
                    return <button key={ratio} type="button" data-action="compaction.threshold" data-value={String(ratio)} disabled={busy || off} onClick={() => { setDraft(String(value)); send(value); }}>{Math.round(ratio * 100)}%</button>;
                  })}
                </span>
              </div>
            </div>
          )}

      <div className="lrow">
            <span className="tx">
              <span className="lb">{t("容量保护")}</span>
              <span className="ds">
                {t("模型窗口的 {p}% 自动整理。中转站未提供容量时使用 160k，可在上下文面板改为实际值。", { p: String(Math.round(box.ratio * 100)) })}
              </span>
            </span>
            <span className="sc">{off ? "—" : tokens(capacity)}</span>
      </div>
      {error && <p className="note" data-lvl="warn">{error}</p>}
    </>
  );
}
