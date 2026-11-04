import { t } from "../i18n";
import { useFolded } from "./usefold";

export function RailToggle({ rail, onRail }: { rail: boolean; onRail: () => void }) {
  const drawer = useFolded("scene");
  if (!drawer) return null;
  return (
    <button className="studio-menu" data-action="chrome.rail" onClick={onRail} aria-pressed={rail} aria-label={rail ? t("common.collapse_the_workspace_rail") : t("app.show_the_workspace_rail")}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3 4h10M3 8h10M3 12h10" /></svg>
    </button>
  );
}
