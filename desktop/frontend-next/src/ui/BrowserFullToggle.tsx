import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { ariaChord, chord } from "./keys";
import { TooltipButton } from "./TooltipButton";

export function BrowserFullToggle({ full, onToggle }: { full: boolean; onToggle: () => void }) {
  const label = full ? t("workbench.exit_expanded_workbench") : t("workbench.expand_workbench");
  return (
    <TooltipButton className="workbench-files" data-action="browser.full" aria-pressed={full}
      aria-label={label} aria-keyshortcuts={`${ariaChord("F")}+Shift`}
      title={label} shortcut={chord("Shift+F")} onClick={onToggle}>
      <StudioIcon name="expand" />
    </TooltipButton>
  );
}
