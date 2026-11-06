import { t } from "../i18n";
import { chord } from "./keys";
import { useFolded } from "./usefold";

export function RailToggle({ rail, onRail }: { rail: boolean; onRail: () => void }) {
  const drawer = useFolded("scene");
  if (!drawer && rail) return null;
  return (
    <button className="studio-menu" data-action="chrome.rail" onClick={onRail} title={`${rail ? t("session.collapse_sidebar") : t("session.expand_sidebar")} ${chord("B")}`} aria-pressed={rail} aria-label={rail ? t("common.collapse_the_workspace_rail") : t("app.show_the_workspace_rail")}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="3" width="12" height="10" rx="1.5" /><path d="M6 3v10" /></svg>
    </button>
  );
}
