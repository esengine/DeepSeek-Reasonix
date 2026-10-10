import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { ariaChord, chord } from "./keys";

export function BrowserWideToggle({ wide, onToggle }: { wide: boolean; onToggle: () => void }) {
  const label = wide ? t("恢复浏览器分栏") : t("加宽浏览器");
  return (
    <button className="workbench-files" data-action="browser.wide" aria-pressed={wide}
      aria-label={label} aria-keyshortcuts={`${ariaChord("B")}+Shift`}
      title={`${label} (${chord("Shift+B")})`} onClick={onToggle}>
      <StudioIcon name="panel" />
    </button>
  );
}
