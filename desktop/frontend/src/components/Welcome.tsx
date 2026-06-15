import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts that send
// immediately so a first turn is one click away.

export function Welcome({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  const examples = [
    { title: t("welcome.ex1"), desc: t("welcome.ex1Desc"), mark: "/" },
    { title: t("welcome.ex2"), desc: t("welcome.ex2Desc"), mark: "git" },
    { title: t("welcome.ex4"), desc: t("welcome.ex4Desc"), mark: "!" },
  ];
  return (
    <div className="welcome">
      <div className="welcome__copy">
        <h2 className="welcome__title">
          <span>{t("welcome.title")}</span>
          <strong>{t("welcome.question")}</strong>
        </h2>
      </div>

      <div className="welcome__examples">
        {examples.map((ex) => (
          <button key={ex.title} className="welcome__ex" onClick={() => onPrompt(ex.title)}>
            <span className="welcome__ex-mark">{ex.mark}</span>
            <span className="welcome__ex-copy">
              <strong>{ex.title}</strong>
              <span>{ex.desc}</span>
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
