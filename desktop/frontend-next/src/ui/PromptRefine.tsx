import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
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
export function usePromptRefine(port: Pick<AgentPort, "refinePrompt">, text: string, adopt: (next: string) => void) {
  const [state, setState] = useState<Refine>({ phase: "idle" });
  const pending = useRef<AbortController | null>(null);
  const tipId = useId();
  useEffect(() => () => pending.current?.abort(), []);

  const run = (draft: string) => {
    if (!draft.trim()) return;
    pending.current?.abort();
    const ac = new AbortController();
    pending.current = ac;
    setState({ phase: "working", draft });
    port.refinePrompt(draft, ac.signal).then(
      (out) => !ac.signal.aborted && setState({ phase: "ready", draft, text: out }),
      (e) => !ac.signal.aborted && setState({ phase: "failed", draft, error: reason(e) || t("composer.refining_failed_try_again") }),
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
      aria-label={t("composer.refine_prompt")}
      aria-describedby={tipId}
      aria-busy={working}
      aria-disabled={!text.trim() || working}
      onClick={() => { if (!working) run(text); }}
    >
      <StudioIcon name="spark" />
      <span className="studio-control-tip" id={tipId} role="tooltip">
        <b>{t("composer.refine_prompt")}</b>
        <span>
          {t("composer.rewrite_it_more_clearly_with")} <span style={{ whiteSpace: "nowrap" }}>· Ctrl+Shift+E</span>
        </span>
      </span>
    </button>
  );

  const card =
    state.phase === "idle" ? null : (
      <section className="refine-card" data-phase={state.phase} aria-label={t("composer.refined_prompt")}>
        <header>
          <StudioIcon name="spark" />
          <b>{t("composer.refined_prompt")}</b>
          <small>
            {state.phase !== "working" && state.draft !== text
              ? t("composer.the_box_has_changed_adopting")
              : t("composer.refine_note")}
          </small>
        </header>
        {state.phase === "working" && (
          <p className="refine-wait" role="status">
            <StudioIcon name="spark" />
            {t("composer.refining")}
            <button type="button" className="btn sm" data-weak="" data-action="prompt.discard" onClick={dismiss}>
              {t("common.cancel")}
            </button>
          </p>
        )}
        {state.phase === "failed" && <p className="refine-error" role="alert">{state.error}</p>}
        {state.phase === "ready" && <div className="refine-text">{state.text}</div>}
        {state.phase !== "working" && (
          <footer>
            <s className="refine-original">{t("composer.refine_original", { text: state.draft })}</s>
            <button type="button" className="btn sm" data-weak="" data-action="prompt.discard" onClick={dismiss}>
              {t("composer.discard")}
            </button>
            <button type="button" className="btn" data-action="prompt.refine" onClick={() => run(state.draft)}>
              {state.phase === "failed" ? t("common.retry") : t("composer.try_again")}
            </button>
            {state.phase === "ready" && (
              <button type="button" className="btn" data-primary data-action="prompt.adopt" onClick={take}>
                {t("composer.adopt")}
              </button>
            )}
          </footer>
        )}
      </section>
    );

  return { button, card, onKey };
}
