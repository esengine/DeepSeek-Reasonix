import logoWordmark from "../assets/logo-wordmark.svg";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts that send
// immediately so a first turn is one click away.

export function Welcome({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  const examples = [t("welcome.ex1"), t("welcome.ex2"), t("welcome.ex3"), t("welcome.ex4")];
  return (
    <div className="welcome welcome--brand">
      <span className="welcome__brand">
        <img src={logoWordmark} className="welcome__brand-logo" alt="Reasonix" draggable={false} />
      </span>
      <h2 className="welcome__title">{t("welcome.title")}</h2>
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
        {examples.map((ex) => (
          <button key={ex} className="welcome__ex" onClick={() => onPrompt(ex)}>
            {ex}
          </button>
        ))}
      </div>
    </div>
  );
}
