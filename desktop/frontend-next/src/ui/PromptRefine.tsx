import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { StudioIcon } from "./StudioIcon";

type Refine =
  | { phase: "idle" }
  | { phase: "working"; draft: string }
  | { phase: "ready"; draft: string; text: string }
  | { phase: "failed"; draft: string; error: string };

// A rewrite of the draft, offered rather than applied: the draft stays in the
// box until the person takes the rewrite, and leaving it costs nothing.
export function usePromptRefine(port: AgentPort, text: string, adopt: (next: string) => void) {
  const [state, setState] = useState<Refine>({ phase: "idle" });
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);

  const run = (draft: string) => {
    if (!draft.trim()) return;
    pending.current?.abort();
    const ac = new AbortController();
    pending.current = ac;
    setState({ phase: "working", draft });
    port.refinePrompt(draft, ac.signal).then(
      (out) => !ac.signal.aborted && setState({ phase: "ready", draft, text: out }),
      (e) => !ac.signal.aborted && setState({ phase: "failed", draft, error: reason(e) || t("优化失败，请重试") }),
    );
  };
  const dismiss = () => {
    pending.current?.abort();
    pending.current = null;
    setState({ phase: "idle" });
  };
  const take = () => {
    if (state.phase !== "ready") return;
    adopt(state.text);
    dismiss();
  };

  // Ctrl+Shift+E asks from the keyboard; Escape puts an open card away before
  // it reaches anything else the box does with it.
  const onKey = (e: KeyboardEvent): boolean => {
    if (e.key.toLowerCase() === "e" && e.shiftKey && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      run(text);
      return true;
    }
    if (e.key === "Escape" && state.phase !== "idle") {
      e.preventDefault();
      dismiss();
      return true;
    }
    return false;
  };

  const working = state.phase === "working";
  const button = (
    <button
      type="button"
      className="mode plain studio-attach studio-refine"
      data-action="prompt.refine"
      aria-label={t("优化提示词")}
      aria-busy={working}
      disabled={!text.trim() || working}
      onClick={() => run(text)}
    >
      <StudioIcon name="spark" />
      <span className="studio-control-tip" role="tooltip">
        <b>{t("优化提示词")}</b>
        <span>{t("用当前模型改写得更清楚，采用前不会替换原文 · Ctrl+Shift+E")}</span>
      </span>
    </button>
  );

  const card =
    state.phase === "idle" ? null : (
      <section className="refine-card" data-phase={state.phase} aria-label={t("优化后的提示词")}>
        <header>
          <StudioIcon name="spark" />
          <b>{t("优化后的提示词")}</b>
          {state.phase !== "working" && state.draft !== text && <small>{t("输入框已修改，采用会覆盖当前内容")}</small>}
        </header>
        {state.phase === "working" && <p className="refine-wait" role="status">{t("正在优化…")}</p>}
        {state.phase === "failed" && <p className="refine-error" role="alert">{state.error}</p>}
        {state.phase === "ready" && <div className="refine-text">{state.text}</div>}
        <footer>
          {state.phase === "ready" && (
            <button type="button" className="btn sm" data-primary data-action="prompt.adopt" onClick={take}>
              {t("采用")}
            </button>
          )}
          {state.phase !== "working" && (
            <button type="button" className="btn sm" data-action="prompt.refine" onClick={() => run(state.draft)}>
              {state.phase === "failed" ? t("重试") : t("再来一次")}
            </button>
          )}
          <button type="button" className="btn sm" data-weak="" data-action="prompt.discard" onClick={dismiss}>
            {state.phase === "working" ? t("取消") : t("放弃")}
          </button>
        </footer>
      </section>
    );

  return { button, card, onKey };
}
