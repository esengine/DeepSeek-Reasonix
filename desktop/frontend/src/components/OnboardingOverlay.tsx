import { useCallback, useRef, useState } from "react";
import logo from "../assets/logo.svg";
import { useT } from "../lib/i18n";
import { app, openExternal } from "../lib/bridge";

// OnboardingOverlay is the full-window first-run gate. Shown by App when
// NeedsOnboarding() returns true (the default provider's API key is unset).
// It validates the pasted key against the provider's balance endpoint, writes
// it to ./.env via Go, and triggers a controller rebuild — the overlay stays
// up until App's useController hook picks up agent:ready and the parent
// drops needsOnboarding back to false. Three states the UI walks through:
//
//   idle        → paste a key, click "Connect & start"
//   validating  → button shows a spinner, the input is locked
//   error       → red banner with the kernel's reason; user can retry
//
// We deliberately don't auto-focus the input on mount: the user may be reading
// the privacy / "where to get a key" copy first, and yanking focus before
// they've decided feels pushy.
export function OnboardingOverlay({ onComplete }: { onComplete: () => void }) {
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
      // The Go side rebuilds the controller and emits agent:ready; the
      // parent's useController hook will pick that up and refresh Meta/
      // History. We just need to unmount ourselves so the main UI takes over.
      // If the rebuild throws the key is still persisted and the next
      // controller rebuild will pick it up, so leaving the overlay showing
      // on error is the safer behavior.
      onComplete();
    } catch (e) {
      // The kernel returns human-readable errors for the common cases
      // (401 → "invalid key", dial failure → "network unreachable"). Map the
      // 401/403 status text to a friendlier i18n key when we recognize it.
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
    <div className="onboarding">
      <div className="onboarding__card">
        <img src={logo} className="onboarding__logo" alt="Reasonix" />
        <div className="onboarding__title">{t("onboarding.title")}</div>
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
          onClick={onComplete}
          disabled={state === "validating"}
        >
          {t("onboarding.skip")}
        </button>
      </div>
    </div>
  );
}
