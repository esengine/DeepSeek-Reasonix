import { useCallback, useRef, useState } from "react";
import logo from "../assets/logo.svg";
import { useT } from "../lib/i18n";
import { app, openExternal } from "../lib/bridge";

// Full-window first-run guide: DeepSeek stays the fastest path, while users can
// open the provider settings or defer setup without being trapped in the gate.
export function OnboardingOverlay({
  onComplete,
  onChooseProvider,
  onSkip,
}: {
  onComplete: () => void;
  onChooseProvider: () => void;
  onSkip: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState("");
  const [state, setState] = useState<"idle" | "validating" | "error">("idle");
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const submit = useCallback(async () => {
    const key = value.trim();
    if (!key) {
      setError(t("onboarding.error.empty"));
      setState("error");
      inputRef.current?.focus();
      return;
    }
    setState("validating");
    setError(null);
    try {
      await app.ConnectKey(key);
      onComplete();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (/status\s*401|status\s*403|invalid/i.test(msg)) {
        setError(t("onboarding.error.invalid"));
      } else if (/network|unreachable|timeout|dial/i.test(msg)) {
        setError(t("onboarding.error.network"));
      } else {
        setError(msg || t("onboarding.error.unknown"));
      }
      setState("error");
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [t, value, onComplete]);

  return (
    <div className="onboarding" role="dialog" aria-modal="true" aria-labelledby="onboarding-title">
      <div className="onboarding__card">
        <img src={logo} className="onboarding__logo" alt="Reasonix" draggable={false} />
        <div id="onboarding-title" className="onboarding__title">{t("onboarding.title")}</div>
        <div className="onboarding__tag">{t("onboarding.tagline")}</div>

        <label className="onboarding__label" htmlFor="onboarding-key">
          {t("onboarding.inputLabel")}
        </label>
        <input
          id="onboarding-key"
          ref={inputRef}
          className="onboarding__input"
          type="password"
          autoComplete="off"
          spellCheck={false}
          autoFocus
          placeholder={t("onboarding.inputPlaceholder")}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            if (state === "error") setState("idle");
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && state !== "validating") {
              e.preventDefault();
              void submit();
            }
          }}
          disabled={state === "validating"}
        />

        {state === "error" && error && (
          <div className="onboarding__error" role="alert">
            {error}
          </div>
        )}

        <button
          className="onboarding__submit"
          onClick={() => void submit()}
          disabled={state === "validating"}
        >
          {state === "validating" ? (
            <>
              <span className="onboarding__spinner" />
              {t("onboarding.validating")}
            </>
          ) : (
            t("onboarding.submit")
          )}
        </button>

        <button
          type="button"
          className="onboarding__provider"
          onClick={onChooseProvider}
          disabled={state === "validating"}
        >
          {t("onboarding.chooseProvider")}
        </button>

        <div className="onboarding__links">
          <button
            type="button"
            className="onboarding__link"
            onClick={() => openExternal("https://platform.deepseek.com/api_keys")}
          >
            {t("onboarding.getKey")}
          </button>
          <span className="onboarding__sep">·</span>
          <span className="onboarding__privacy">{t("onboarding.privacy")}</span>
        </div>

        <button
          type="button"
          className="onboarding__skip"
          onClick={onSkip}
          disabled={state === "validating"}
        >
          {t("onboarding.skip")}
        </button>
      </div>
    </div>
  );
}
