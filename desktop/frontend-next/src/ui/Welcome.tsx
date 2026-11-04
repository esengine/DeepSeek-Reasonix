import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import logoWordmark from "../assets/logo-wordmark.svg";

interface Props {
  // full plays the whole sequence; short stops after the introduction, for a
  // machine that already has a key and nothing to ask for.
  variant: "full" | "short";
  // The connection card, when there is one. It rises inside this scene after
  // the collapse rather than replacing it: the introduction has to still be on
  // screen when the first question is asked, or the two read as unrelated.
  children?: React.ReactNode;
  // Whether the sequence has already played on this machine. A second visit
  // with a key still to enter shows the card immediately, over a still scene.
  replay: boolean;
  onDone: () => void;
}

// The beat the collapse happens on. The card fades in just after it, so the
// brand never leaves the screen between the introduction and the form.
const COLLAPSE_MS = 9600;
const SHORT_MS = 6200;

// The opening scene keeps a dark ground across application themes.
export function Welcome({ variant, children, replay, onDone }: Props) {
  const [leaving, setLeaving] = useState(false);
  const done = useRef(false);
  const holds = children != null;

  // One exit for every route out: the timer, a key, a click, or reduced motion.
  useEffect(() => {
    // With a card to show, the sequence ending is not the end of anything: the
    // scene stays and the card takes over. Only a sequence with nothing behind
    // it dismisses itself.
    const finish = () => {
      if (done.current || holds) return;
      done.current = true;
      setLeaving(true);
      window.setTimeout(onDone, 380);
    };
    if (!replay) return;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const at = reduced ? 0 : variant === "full" ? COLLAPSE_MS : SHORT_MS;
    const timer = window.setTimeout(finish, at);
    const skip = () => finish();
    window.addEventListener("keydown", skip);
    window.addEventListener("pointerdown", skip);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("keydown", skip);
      window.removeEventListener("pointerdown", skip);
    };
  }, [variant, onDone, holds, replay]);

  return (
    <div
      className="oobe"
      data-play={replay && !leaving ? "" : undefined}
      data-held={holds ? "" : undefined}
      data-leaving={leaving ? "" : undefined}
    >
      <div className="oobe-glow g1" />
      <div className="oobe-glow g2" />
      <div className="oobe-glow g3" />
      <div className="oobe-vig" />

      <div className="oobe-scene">
        <div className="oobe-lockup">
          <img className="oobe-mark" src={logoWordmark} alt="Reasonix" draggable={false} />
          <h1 className="oobe-brand" aria-label="Reasonix Studio">studio</h1>
        </div>
        <p className="oobe-sub">coding agent</p>

        <div className="oobe-lines" aria-live="polite">
          <div className="oobe-line l1">
            <b>{t("onboarding.welcome_to_reasonix")}</b>
          </div>
          <div className="oobe-line l2">
            <b>{t("onboarding.you_hand_it_a_task")}</b>
            <span>{t("onboarding.reading_code_checking_facts_doing")}</span>
          </div>
          <div className="oobe-line l3">
            <b>{t("onboarding.every_step_stays_on_the")}</b>
            <span>{t("onboarding.which_file_changed_which_command")}</span>
          </div>
        </div>
      </div>

      {children}

      {replay && !holds && (
        <>
          <div className="oobe-prog"><i /></div>
          <span className="oobe-skip">{t("onboarding.press_any_key_to_skip")}</span>
        </>
      )}
    </div>
  );
}
