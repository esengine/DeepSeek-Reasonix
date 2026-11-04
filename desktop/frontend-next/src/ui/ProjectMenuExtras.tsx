import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function ProjectMenuExtras({ canSelect, selecting, onSelect, dismiss }: { canSelect: boolean; selecting: boolean; onSelect: () => void; dismiss: () => void }) {
  return (
    <div className="session-pop-group">
      <button role="menuitem" data-action="session.selection" data-value="start" disabled={!canSelect || selecting} onClick={() => { dismiss(); onSelect(); }}>
        <StudioIcon name="check" /><span>{t("session.select_local_sessions")}</span>
      </button>
    </div>
  );
}
