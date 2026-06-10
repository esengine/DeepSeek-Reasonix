import logoSymbol from "../assets/logo-symbol.svg";
import { useT } from "../lib/i18n";

export function Welcome({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  void onPrompt;

  return (
    <div className="welcome">
      <div className="welcome__brand" aria-label="Reasonix">
        <img src={logoSymbol} className="welcome__mark" alt="" />
        <span>Reasonix</span>
      </div>
      <h1 className="welcome__title">{t("welcome.title")}</h1>
    </div>
  );
}
