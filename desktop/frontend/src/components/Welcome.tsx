import logoWordmark from "../assets/logo-wordmark.svg";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts that send
// immediately so a first turn is one click away.

export function Welcome({ onPrompt, variant = "default" }: { onPrompt: (text: string) => void; variant?: "default" | "creation" }) {
  const t = useT();
  if (variant === "creation") {
    const cards = [
      {
        icon: "plan",
        title: t("welcome.creation.explainTitle"),
        body: t("welcome.creation.explainBody"),
      },
      {
        icon: "html",
        title: t("welcome.creation.gitTitle"),
        body: t("welcome.creation.gitBody"),
      },
      {
        icon: "think",
        title: t("welcome.creation.bugTitle"),
        body: t("welcome.creation.bugBody"),
      },
    ];
    return (
      <div className="welcome welcome--creation">
        <h2 className="welcome-creation__headline">
          <span>{t("welcome.creation.titlePrimary")}</span>
          <span>{t("welcome.creation.titleSecondary")}</span>
        </h2>
        <div className="welcome-creation__cards">
          {cards.map((card) => (
            <button key={card.title} className="welcome-creation__card" onClick={() => onPrompt(card.title)}>
              <span className="welcome-creation__icon">{card.icon}</span>
              <strong>{card.title}</strong>
              <span>{card.body}</span>
            </button>
          ))}
        </div>
      </div>
    );
  }

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
