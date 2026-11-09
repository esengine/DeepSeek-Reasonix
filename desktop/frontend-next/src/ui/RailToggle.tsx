import { StudioIcon } from "./StudioIcon";
import { t } from "../i18n";
import { chord } from "./keys";
import { useFolded } from "./usefold";

export function RailToggle({ rail, onRail }: { rail: boolean; onRail: () => void }) {
  const drawer = useFolded("scene");
  const phone = useFolded("phone");
  if (!drawer && rail) return null;
  // On a phone the rail is the list of sessions, and the control for it says so.
  const label = phone ? t("app.session_list") : rail ? t("common.collapse_the_workspace_rail") : t("app.show_the_workspace_rail");
  return (
    <button className="studio-menu" data-action="chrome.rail" onClick={onRail} title={phone ? label : `${rail ? t("session.collapse_sidebar") : t("session.expand_sidebar")} ${chord("B")}`} aria-pressed={rail} aria-label={label}>
      {phone
        ? <StudioIcon name="menu" />
        : <StudioIcon name="panel" />}
    </button>
  );
}
