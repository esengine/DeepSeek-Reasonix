import { t } from "../i18n";
import { SymbolMark } from "./RMark";

interface HeroProps {
  needsProject: boolean;
  onOpen: () => void;
  onKeep: () => void;
  onSuggest?: (text: string) => void;
}

export function Hero({ needsProject, onOpen, onKeep, onSuggest }: HeroProps) {
  return (
    <div className="hero">
      <SymbolMark />
      <div className="t">{needsProject ? t("chat.open_a_project_first") : t("chat.describe_the_task_reasonix_takes")}</div>
      <div className="s">
        {needsProject
          ? t("chat.reading_code_running_tests_and")
          : t("chat.it_can_read_code_research")}
      </div>
      {needsProject ? (
        <div className="herogo">
          <button className="pick" data-action="workspace.add" onClick={onOpen}>
            {t("chat.open_a_project")}
          </button>
          <button className="stay" onClick={onKeep}>
            {t("chat.stay_where_i_am")}
          </button>
        </div>
      ) : onSuggest ? (
        <div className="herostart">
          {[t("chat.starter_explain_the_project"), t("chat.starter_find_the_regression"), t("chat.starter_write_tests")].map((text) => (
            <button key={text} type="button" data-action="hero.suggest" onClick={() => onSuggest(text)}>{text}</button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
