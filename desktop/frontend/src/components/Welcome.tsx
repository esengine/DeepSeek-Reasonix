import { useEffect, useState } from "react";
import logoWordmark from "../assets/logo-wordmark.svg";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: brand, a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts.
//
// On mount the component calls GenerateWelcomePrompts, which reads the
// codegraph index (.codegraph/codegraph.db) to produce three Chinese prompts
// tailored to the workspace.  While the call is in flight (or if it fails),
// the fixed i18n examples are shown so the user never sees a blank state.

function parsePrompts(raw: string): string[] | null {
  if (!raw) return null;
  try {
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr) || arr.length !== 3) return null;
    if (!arr.every((s) => typeof s === "string" && s.length > 0)) return null;
    return arr as string[];
  } catch {
    return null;
  }
}

export function Welcome({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  const fallback = [t("welcome.ex1"), t("welcome.ex2"), t("welcome.ex3")];
  const [prompts, setPrompts] = useState<string[]>(fallback);

  useEffect(() => {
    setPrompts(fallback);
    app
      .GenerateWelcomePrompts()
      .then((raw) => {
        const parsed = parsePrompts(raw);
        if (parsed) setPrompts(parsed);
      })
      .catch(() => {});
  }, []);

  return (
    <div className="welcome">
      <img src={logoWordmark} className="welcome__logo" alt="Reasonix" />
      <div className="welcome__tag">{t("welcome.tagline")}</div>

      <div className="welcome__hints">
        <span>
          <kbd>/</kbd> {t("welcome.hintCommands")}
        </span>
        <span>
          <kbd>@</kbd> {t("welcome.hintFiles")}
        </span>
        <span>
          <kbd>⏎</kbd> {t("welcome.hintSend")}
        </span>
      </div>

      <div className="welcome__examples">
        {prompts.map((ex) => (
          <button key={ex} className="welcome__ex" onClick={() => onPrompt(ex)}>
            {ex}
          </button>
        ))}
      </div>
    </div>
  );
}
