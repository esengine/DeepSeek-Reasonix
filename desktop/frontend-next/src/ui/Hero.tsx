import { t } from "../i18n";
import { SymbolMark } from "./RMark";

interface HeroProps {
  needsProject: boolean;
  onOpen: () => void;
  onKeep: () => void;
}

export function Hero({ needsProject, onOpen, onKeep }: HeroProps) {
  return (
    <div className="hero">
      <SymbolMark />
      <div className="t">{needsProject ? t("hero.openAProjectFirst") : t("hero.describeTheTaskReasonixTakes")}</div>
      <div className="s">
        {needsProject
          ? t("hero.readingCodeRunningTestsAnd")
          : t("hero.itCanReadCodeResearch")}
      </div>
      {needsProject ? (
        <div className="herogo">
          <button className="pick" data-action="workspace.add" onClick={onOpen}>
            {t("hero.openAProject")}
          </button>
          <button className="stay" onClick={onKeep}>
            {t("hero.stayWhereIAm")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
