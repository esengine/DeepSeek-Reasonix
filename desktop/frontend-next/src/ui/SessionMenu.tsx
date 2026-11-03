import { forwardRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { TreeSession } from "../port/hub";
import { StudioIcon } from "./StudioIcon";

export interface SessionMenuProps {
  session: TreeSession;
  label: string;
  /** The conversation behind this row is running a turn. */
  live: boolean;
  pinned: boolean;
  exporting: boolean;
  autoNaming: boolean;
  onPin: () => void;
  /** Absent where the row is not unread. */
  onMarkRead?: () => void;
  onRename: () => void;
  onAutoName: () => void;
  onPause: () => void;
  onClose: () => void;
  onArchive: () => void;
  onBranch?: () => void;
  onCopyInfo: () => void;
  onExport: (format: "json" | "md") => void;
  onDelete: () => void;
  /** One place up or down in the project list; absent where the row has no order to move in. */
  onMove?: (direction: -1 | 1) => void;
  canMove?: { up: boolean; down: boolean };
  /** Opens the choice of another project; absent where a conversation cannot move (a copy, a version). */
  onMoveTo?: () => void;
}

/** What can be done to one conversation, drawn at the document root so the
 *  sidebar's clipping cannot cut it. Every action closes the menu itself:
 *  the parent's handlers do that, this only says which one was chosen. */
export const SessionMenu = forwardRef<HTMLDivElement, SessionMenuProps>(function SessionMenu(
  { session, label, live, pinned, exporting, autoNaming, onPin, onMarkRead, onRename, onAutoName, onPause, onClose, onArchive, onBranch, onCopyInfo, onExport, onDelete, onMove, canMove, onMoveTo },
  ref,
) {
  const target = session.path;
  return createPortal(
    <div ref={ref} className="session-pop" role="menu" aria-label={t("common.sessionActions")} style={{ maxHeight: "calc(100vh / var(--zoom, 1) - 24px)", overflowY: "auto" }} onClick={(ev) => ev.stopPropagation()}>
      <div className="session-pop-head">
        <b>{label}</b>
        <small>{live ? t("sessionMenu.running") : t("common.done")} · {t("sessionMenu.localWorkspace")}</small>
      </div>
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.pin" data-target={target} onClick={onPin}>
          <StudioIcon name="pin" /><span>{pinned ? t("common.unpin") : t("common.pinSession")}</span>
        </button>
        {onMarkRead && (
          <button role="menuitem" data-action="session.mark-read" data-target={target} onClick={onMarkRead}>
            <StudioIcon name="check" /><span>{t("common.markAsRead")}</span>
          </button>
        )}
        {onMove && canMove && ([-1, 1] as const).map((direction) => (
          <button key={direction} role="menuitem" data-action="session.move" data-target={target} data-value={direction}
            disabled={direction === -1 ? !canMove.up : !canMove.down} onClick={() => onMove(direction)}>
            <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} /><span>{t(direction === -1 ? "common.moveUp" : "common.moveDown")}</span>
          </button>
        ))}
        <button role="menuitem" data-action="session.rename-start" data-target={target} onClick={onRename}>
          <StudioIcon name="edit" /><span>{t("common.renamed")}</span>
        </button>
        <button role="menuitem" data-action="session.auto-name" data-target={target} disabled={autoNaming} onClick={onAutoName}>
          <StudioIcon name="spark" /><span>{autoNaming ? t("common.naming") : t("common.autoName")}</span>
        </button>
        {session.runtimeId && live && (
          <button role="menuitem" data-action="session.pause" data-target={target} onClick={onPause}>
            <StudioIcon name="pause" /><span>{t("sessionMenu.pauseRun")}</span>
          </button>
        )}
        {session.runtimeId && (
          <button
            role="menuitem"
            data-action="session.close"
            data-target={target}
            disabled={live}
            title={live ? t("sessionMenu.runningPauseItBeforeClosing") : undefined}
            onClick={onClose}
          >
            <StudioIcon name="close" /><span>{t("sessionMenu.closeConversation")}</span>
          </button>
        )}
        {session.runtimeId && onBranch && (
          <button role="menuitem" data-action="session.branch" data-target={target} onClick={onBranch}>
            <StudioIcon name="branch" /><span>{t("sessionMenu.branchFromTheLatestReply")}</span>
          </button>
        )}
        <button
          role="menuitem"
          data-action="session.archive"
          data-target={target}
          data-value={session.archived ? "restore" : "archive"}
          disabled={!!session.runtimeId && live}
          onClick={onArchive}
        >
          <StudioIcon name="archive" /><span>{t(session.archived ? "common.unarchive" : "common.archiveSession")}</span>
        </button>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.copy-info" data-target={target} onClick={onCopyInfo}>
          <StudioIcon name="copy" /><span>{t("sessionMenu.copySessionInformation")}</span>
        </button>
        {onMoveTo && (
          <button role="menuitem" data-action="session.move-to-start" data-target={target} disabled={!!session.runtimeId} title={session.runtimeId ? t("sessionMenu.closeThisConversationFirstThen") : undefined} onClick={onMoveTo}>
            <StudioIcon name="folder" /><span>{t("sessionMenu.moveToAProject")}</span>
          </button>
        )}
        <button role="menuitem" data-action="session.export" data-target={target} disabled={exporting} onClick={() => onExport("json")}>
          <StudioIcon name="download" /><span>{t("sessionMenu.exportSession")}</span><small>JSON</small>
        </button>
        <button role="menuitem" data-action="session.export" data-value="md" data-target={target} disabled={exporting} onClick={() => onExport("md")}>
          <StudioIcon name="file" /><span>{t("sessionMenu.exportSession")}</span><small>MD</small>
        </button>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        <button className="danger" role="menuitem" data-action="session.delete" data-target={target} onClick={onDelete}>
          <StudioIcon name="trash" /><span>{t("common.deleteSession")}</span>
        </button>
      </div>
    </div>,
    document.body,
  );
});
