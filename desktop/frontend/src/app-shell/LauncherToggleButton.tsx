import { ClipboardList } from "lucide-react";
import { Tooltip } from "../components/Tooltip";
import type { Translator } from "../lib/i18n";

// Floating launcher card show/hide toggle, rendered in the topic bar's actions
// row. Only mounted while the card can actually show (dock collapsed, surface
// wide enough), so it is never a control that swallows its own click; the
// pressed state mirrors whether the card is currently on screen.
export function LauncherToggleButton({ visible, t, onToggle }: {
  visible: boolean;
  t: Translator;
  onToggle: () => void;
}) {
  return (
    <div className="app__launcher-toggle">
      <Tooltip label={visible ? t("rightDock.hideLauncher") : t("rightDock.showLauncher")}>
        <button
          className={[
            "topicbar__chrome-btn",
            "topicbar__chrome-btn--launcher",
            visible ? "topicbar__chrome-btn--active" : "",
          ].filter(Boolean).join(" ")}
          type="button"
          onClick={onToggle}
          aria-label={visible ? t("rightDock.hideLauncher") : t("rightDock.showLauncher")}
          aria-pressed={visible}
        >
          <ClipboardList size={15} />
        </button>
      </Tooltip>
    </div>
  );
}
