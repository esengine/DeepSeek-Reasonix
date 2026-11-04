import { forwardRef, useId } from "react";
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
  keptCount?: number;
  keptOpen?: boolean;
  onKept?: () => void;
}

/** What can be done to one conversation, drawn at the document root so the
 *  sidebar's clipping cannot cut it. Every action closes the menu itself:
 *  the parent's handlers do that, this only says which one was chosen. */
export const SessionMenu = forwardRef<HTMLDivElement, SessionMenuProps>(function SessionMenu(
  { session, label, live, pinned, exporting, autoNaming, onPin, onMarkRead, onRename, onAutoName, onPause, onClose, onArchive, onBranch, onCopyInfo, onExport, onDelete, onMove, canMove, onMoveTo, keptCount = 0, keptOpen, onKept },
  ref,
) {
  const target = session.path;
  const why = useId();
  return createPortal(
    <div ref={ref} className="session-pop" role="menu" aria-label={t("common.session_actions")} style={{ maxHeight: "calc(100vh / var(--zoom, 1) - 24px)", overflowY: "auto" }} onClick={(ev) => ev.stopPropagation()}>
      <div className="session-pop-head">
        <b>{label}</b>
        <small>{live ? t("session.running") : t("common.done_2")} · {t("session.local_workspace")}</small>
      </div>
      {session.runtimeId && live && (
        <>
          <div className="session-pop-group">
            <button role="menuitem" data-action="session.pause" data-target={target} onClick={onPause}>
              <StudioIcon name="pause" /><span>{t("session.pause_run")}</span>
            </button>
          </div>
          <div className="session-pop-divider" />
        </>
      )}
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.pin" data-target={target} onClick={onPin}>
          <StudioIcon name="pin" /><span>{pinned ? t("common.unpin") : t("common.pin_session")}</span>
        </button>
        {onMove && canMove && ([-1, 1] as const).map((direction) => (
          <button key={direction} role="menuitem" data-action="session.move" data-target={target} data-value={direction}
            disabled={direction === -1 ? !canMove.up : !canMove.down} onClick={() => onMove(direction)}>
            <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} /><span>{t(direction === -1 ? "common.move_up" : "common.move_down")}</span>
          </button>
        ))}
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.rename-start" data-target={target} onClick={onRename}>
          <StudioIcon name="edit" /><span>{t("common.renamed")}</span>
        </button>
        <button role="menuitem" data-action="session.auto-name" data-target={target} disabled={autoNaming} onClick={onAutoName}>
          <StudioIcon name="spark" /><span>{autoNaming ? t("session.naming") : t("session.auto_name")}</span>
        </button>
        {onMarkRead && (
          <button role="menuitem" data-action="session.mark-read" data-target={target} onClick={onMarkRead}>
            <StudioIcon name="check" /><span>{t("common.mark_as_read")}</span>
          </button>
        )}
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        {session.runtimeId && onBranch && (
          <button role="menuitem" data-action="session.branch" data-target={target} onClick={onBranch}>
            <StudioIcon name="branch" /><span>{t("session.branch_from_the_latest_reply")}</span>
          </button>
        )}
        <button role="menuitem" data-action="session.copy-info" data-target={target} onClick={onCopyInfo}>
          <StudioIcon name="copy" /><span>{t("session.copy_session_information")}</span>
        </button>
        <div className="session-pop-export" role="group" aria-label={t("session.export_session")}>
          <StudioIcon name="download" /><span>{t("session.export_session")}</span>
          <button role="menuitem" aria-label={`${t("session.export_session")} · Markdown`} data-action="session.export" data-value="md" data-target={target} disabled={exporting} onClick={() => onExport("md")}>Markdown</button>
          <button role="menuitem" aria-label={`${t("session.export_session")} · JSON`} data-action="session.export" data-value="json" data-target={target} disabled={exporting} onClick={() => onExport("json")}>JSON</button>
        </div>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        {keptCount > 0 && onKept && (
          <button role="menuitem" data-action="session.kept" data-target={target} aria-expanded={keptOpen} onClick={onKept}>
            <StudioIcon name="clock" /><span>{t("session.earlier_versions_n", { n: keptCount })}</span>
          </button>
        )}
        {onMoveTo && (
          <button role="menuitem" aria-label={t("session.move_to_a_project")} aria-describedby={session.runtimeId ? `${why}-move` : undefined} data-action="session.move-to-start" data-target={target} disabled={!!session.runtimeId} onClick={onMoveTo}>
            <StudioIcon name="folder" /><span>{t("session.move_to_a_project")}{session.runtimeId && <small className="session-pop-why" id={`${why}-move`}>{t("session.close_this_conversation_first_then")}</small>}</span>
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
          <StudioIcon name="archive" /><span>{t(session.archived ? "common.unarchive" : "common.archive_session")}</span>
        </button>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        {session.runtimeId && (
          <button role="menuitem" aria-label={t("session.close_conversation")} aria-describedby={live ? `${why}-close` : undefined} data-action="session.close" data-target={target} disabled={live} onClick={onClose}>
            <StudioIcon name="close" /><span>{t("session.close_conversation")}{live && <small className="session-pop-why" id={`${why}-close`}>{t("session.running_pause_it_before_closing")}</small>}</span>
          </button>
        )}
        <button className="danger" role="menuitem" data-action="session.delete" data-target={target} onClick={onDelete}>
          <StudioIcon name="trash" /><span>{t("common.delete_session")}</span>
        </button>
      </div>
    </div>,
    document.body,
  );
});
