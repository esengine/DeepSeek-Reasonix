import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";

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

// Welcome is the opening sequence: aurora on a fixed dark ground, the R drawn
// and then traced by a light, the wordmark swept by a highlight, three lines
// of introduction, and a collapse that turns the whole group into the header
// of whatever comes next. It plays once per machine.
//
// The ground is dark whatever the app theme is. This is a ceremony rather than
// a screen of the app, the way an OS out-of-box sequence does not follow the
// user's colour scheme.
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
        <svg className="oobe-mark" viewBox="0 0 16 16" aria-hidden="true">
          <path className="base" pathLength={100} d="M4.7 2.9V13.1" />
          <path className="base" pathLength={100} d="M4.7 2.9h4.2a2.9 2.9 0 0 1 0 5.8H4.7" />
          <path className="base" pathLength={100} d="M9 8.7l3.3 4.4" />
          <path className="lume" pathLength={100} d="M4.7 13.1V2.9h4.2a2.9 2.9 0 0 1 0 5.8H4.7l3.6 4.4" />
        </svg>
        <h1 className="oobe-brand">Reasonix Studio</h1>
        <p className="oobe-sub">coding agent</p>

        <div className="oobe-lines" aria-live="polite">
          <div className="oobe-line l1">
            <b>{t("欢迎使用 Reasonix。")}</b>
          </div>
          <div className="oobe-line l2">
            <b>{t("交付一项任务，由它完整执行。")}</b>
            <span>{t("读取代码、查证、执行、验证 —— 而非仅给出一段建议。")}</span>
          </div>
          <div className="oobe-line l3">
            <b>{t("每一步均可追溯。")}</b>
            <span>{t("修改了哪个文件、运行了哪条命令、消耗了多少 token，均可回溯查看。")}</span>
          </div>
        </div>
      </div>

      {children}

      {replay && !holds && (
        <>
          <div className="oobe-prog"><i /></div>
          <span className="oobe-skip">{t("按任意键跳过")}</span>
        </>
      )}
    </div>
  );
}
