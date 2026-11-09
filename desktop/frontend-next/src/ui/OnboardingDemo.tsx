import { StudioIcon } from "./StudioIcon";
import { useEffect, useState } from "react";
import { t } from "../i18n";
import { RMark } from "./RMark";

const CYCLE_MS = 7000;
const calm = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

const at = (n: number) => ({ "--n": n }) as React.CSSProperties;

// A seven-second loop of what a session looks like: a request, the model
// working, a read, an edit, a run, and the result. It pauses under the pointer,
// a click shows the last frame, and reduced motion starts there.
export function OnboardingDemo() {
  const [final, setFinal] = useState(calm);
  const [held, setHeld] = useState(false);
  const [round, setRound] = useState(0);
  useEffect(() => {
    if (final || held) return;
    const timer = window.setTimeout(() => setRound((n) => n + 1), CYCLE_MS);
    return () => window.clearTimeout(timer);
  }, [final, held, round]);
  return (
    <div className="onb-demo" aria-hidden="true" data-final={final ? "" : undefined} data-held={held ? "" : undefined}
      data-action-mouseenter="onboarding.demo" data-action-mouseleave="onboarding.demo" data-action-click="onboarding.demo"
      onMouseEnter={() => setHeld(true)} onMouseLeave={() => setHeld(false)} onClick={() => setFinal(true)}>
      <div className="demo-run" key={round}>
        <p className="d me" style={at(0)}>{t("onboarding.demo_ask")}</p>
        <p className="d think" style={at(1)}><RMark className="rmark demo-spin" />{t("onboarding.demo_thinking")}</p>
        <p className="d tool" style={at(3)}><b>{t("onboarding.demo_read")}</b><code>scripts/export.py</code><small>48</small></p>
        <p className="d tool" style={at(4)}><b>{t("onboarding.demo_edit")}</b><code>scripts/export.py</code><small className="add">+12 −3</small></p>
        <p className="d tool" style={at(5)}><b>{t("onboarding.demo_run")}</b><code>pytest</code><small className="ok">12 <StudioIcon name="check" /></small></p>
        <p className="d done" style={at(6)}>{t("onboarding.demo_done")}</p>
      </div>
    </div>
  );
}
