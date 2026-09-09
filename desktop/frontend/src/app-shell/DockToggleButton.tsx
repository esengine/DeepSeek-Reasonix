import { PanelRight } from "lucide-react";
import { Tooltip } from "../components/Tooltip";
import type { Translator } from "../lib/i18n";

// Dock collapse/expand toggle. Rendered in the dock's own tools row when the
// dock is open (its top-right corner), and in the topic bar when closed.
export function DockToggleButton({ renderable, t, onToggle }: { renderable: boolean; t: Translator; onToggle: () => void }) {
  return (
    <Tooltip label={renderable ? t("rightDock.collapse") : t("rightDock.expand")}>
      <button
        className={[
          "topicbar__chrome-btn",
          "topicbar__chrome-btn--workspace",
          renderable ? "topicbar__chrome-btn--active" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={onToggle}
        aria-label={renderable ? t("rightDock.collapse") : t("rightDock.expand")}
        aria-pressed={renderable}
      >
        <PanelRight size={15} />
      </button>
    </Tooltip>
  );
}
