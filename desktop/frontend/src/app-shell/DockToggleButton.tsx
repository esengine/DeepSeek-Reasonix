import { PanelRight, PanelRightClose } from "lucide-react";
import { Tooltip } from "../components/Tooltip";
import type { Translator } from "../lib/i18n";

// Dock collapse/expand toggle, rendered as one of the bar's action buttons. The
// icon alone carries the state — no accent fill, so it reads as a peer of the
// buttons beside it rather than as a highlighted control.
export function DockToggleButton({ renderable, t, onToggle }: { renderable: boolean; t: Translator; onToggle: () => void }) {
  return (
    <Tooltip label={renderable ? t("rightDock.collapse") : t("rightDock.expand")}>
      <button
        className="topicbar__chrome-btn topicbar__chrome-btn--workspace"
        type="button"
        onClick={onToggle}
        aria-label={renderable ? t("rightDock.collapse") : t("rightDock.expand")}
        aria-pressed={renderable}
      >
        {renderable ? <PanelRightClose size={15} /> : <PanelRight size={15} />}
      </button>
    </Tooltip>
  );
}
