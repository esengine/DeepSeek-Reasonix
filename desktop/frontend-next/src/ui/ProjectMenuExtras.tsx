import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function ProjectMenuExtras({ canSelect, selecting, onSelect, onToggleAll, allFolded, canFold, searching, dismiss }: {
  canSelect: boolean;
  selecting: boolean;
  onSelect: () => void;
  onToggleAll?: () => void;
  allFolded?: boolean;
  canFold?: boolean;
  searching?: boolean;
  dismiss: () => void;
}) {
  return (
    <div className="session-pop-group">
      <button role="menuitem" data-action="session.selection" data-value="start" disabled={!canSelect || selecting} onClick={() => { dismiss(); onSelect(); }}>
        <StudioIcon name="check" /><span>{t("选择本机会话")}</span>
      </button>
      {onToggleAll && (
        <button role="menuitem" data-action="workspace.fold-all" disabled={!canFold || searching} aria-label={t(allFolded ? "展开所有项目" : "收起所有项目")} onClick={() => { dismiss(); onToggleAll(); }}>
          <StudioIcon name={allFolded ? "expandAll" : "collapseAll"} /><span>{t(allFolded ? "展开所有项目" : "收起所有项目")}</span>
        </button>
      )}
    </div>
  );
}
