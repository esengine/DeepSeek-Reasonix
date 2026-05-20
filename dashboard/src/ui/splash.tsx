import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";

const SPLASH_FLAG = "reasonix.splash.shown";

export function shouldShowSplash(): boolean {
  try {
    return sessionStorage.getItem(SPLASH_FLAG) !== "1";
  } catch {
    return true;
  }
}

function markSplashShown() {
  try {
    sessionStorage.setItem(SPLASH_FLAG, "1");
  } catch {
    /* sessionStorage unavailable */
  }
}

export function Splash({ onDone }: { onDone: () => void }) {
  const [leaving, setLeaving] = useState(false);
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  useEffect(() => {
    const t1 = window.setTimeout(() => setLeaving(true), 1350);
    const t2 = window.setTimeout(() => {
      markSplashShown();
      onDoneRef.current();
    }, 1800);
    return () => {
      window.clearTimeout(t1);
      window.clearTimeout(t2);
    };
  }, []);

  useEffect(() => {
    const skip = (e: KeyboardEvent) => {
      if (e.key !== "Escape" && e.key !== "Enter" && e.key !== " ") return;
      markSplashShown();
      onDoneRef.current();
    };
    window.addEventListener("keydown", skip);
    return () => window.removeEventListener("keydown", skip);
  }, []);

  const skipClick = () => {
    markSplashShown();
    onDone();
  };

  return (
    <div className="splash" data-leaving={leaving} onClick={skipClick}>
      <div className="splash-card">
        <div className="splash-mark" />
        <div className="splash-name">Reasonix</div>
        <div className="splash-sub">{t("app.splashSubtitle")}</div>
        <div className="splash-dots">
          <span />
          <span />
          <span />
        </div>
      </div>
    </div>
  );
}
