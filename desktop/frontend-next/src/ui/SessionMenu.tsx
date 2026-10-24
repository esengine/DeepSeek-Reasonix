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
    <div ref={ref} className="session-pop" role="menu" aria-label={t("会话操作")} style={{ maxHeight: "calc(100vh / var(--zoom, 1) - 24px)", overflowY: "auto" }} onClick={(ev) => ev.stopPropagation()}>
      <div className="session-pop-head">
        <b>{label}</b>
        <small>{live ? t("执行中") : t("已完成")} · {t("本地工作区")}</small>
      </div>
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.pin" data-target={target} onClick={onPin}>
          <StudioIcon name="pin" /><span>{pinned ? t("取消置顶") : t("置顶会话")}</span>
        </button>
        {onMarkRead && (
          <button role="menuitem" data-action="session.mark-read" data-target={target} onClick={onMarkRead}>
            <StudioIcon name="check" /><span>{t("标为已读")}</span>
          </button>
        )}
        {onMove && canMove && ([-1, 1] as const).map((direction) => (
          <button key={direction} role="menuitem" data-action="session.move" data-target={target} data-value={direction}
            disabled={direction === -1 ? !canMove.up : !canMove.down} onClick={() => onMove(direction)}>
            <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} /><span>{t(direction === -1 ? "上移" : "下移")}</span>
          </button>
        ))}
        <button role="menuitem" data-action="session.rename-start" data-target={target} onClick={onRename}>
          <StudioIcon name="edit" /><span>{t("重命名")}</span>
        </button>
        <button role="menuitem" data-action="session.auto-name" data-target={target} disabled={autoNaming} onClick={onAutoName}>
          <StudioIcon name="spark" /><span>{autoNaming ? t("命名中…") : t("自动命名")}</span>
        </button>
        {session.runtimeId && live && (
          <button role="menuitem" data-action="session.pause" data-target={target} onClick={onPause}>
            <StudioIcon name="pause" /><span>{t("暂停执行")}</span>
          </button>
        )}
        {session.runtimeId && (
          <button
            role="menuitem"
            data-action="session.close"
            data-target={target}
            disabled={live}
            title={live ? t("正在运行，暂停后才能关闭") : undefined}
            onClick={onClose}
          >
            <StudioIcon name="close" /><span>{t("关闭会话")}</span>
          </button>
        )}
        {session.runtimeId && onBranch && (
          <button role="menuitem" data-action="session.branch" data-target={target} onClick={onBranch}>
            <StudioIcon name="branch" /><span>{t("从最近回复分叉")}</span>
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
          <StudioIcon name="archive" /><span>{t(session.archived ? "取消归档" : "归档会话")}</span>
        </button>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.copy-info" data-target={target} onClick={onCopyInfo}>
          <StudioIcon name="copy" /><span>{t("复制会话信息")}</span>
        </button>
        {onMoveTo && (
          <button role="menuitem" data-action="session.move-to-start" data-target={target} disabled={!!session.runtimeId} title={session.runtimeId ? t("先关闭这个会话，再移动它") : undefined} onClick={onMoveTo}>
            <StudioIcon name="folder" /><span>{t("移动到项目…")}</span>
          </button>
        )}
        <button role="menuitem" data-action="session.export" data-target={target} disabled={exporting} onClick={() => onExport("json")}>
          <StudioIcon name="download" /><span>{t("导出会话")}</span><small>JSON</small>
        </button>
        <button role="menuitem" data-action="session.export" data-value="md" data-target={target} disabled={exporting} onClick={() => onExport("md")}>
          <StudioIcon name="file" /><span>{t("导出会话")}</span><small>MD</small>
        </button>
      </div>
      <div className="session-pop-divider" />
      <div className="session-pop-group">
        <button className="danger" role="menuitem" data-action="session.delete" data-target={target} onClick={onDelete}>
          <StudioIcon name="trash" /><span>{t("删除会话")}</span>
        </button>
      </div>
    </div>,
    document.body,
  );
});
