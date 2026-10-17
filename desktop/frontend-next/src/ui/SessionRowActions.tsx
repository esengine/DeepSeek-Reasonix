import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

interface Target {
  target: string;
  title: string;
  disabled?: boolean;
}

export function SessionPinAction({ target, title, pinned, enabled, disabled, onPin }: Target & {
  pinned: boolean;
  enabled: boolean;
  onPin: () => void;
}) {
  if (!enabled) return pinned ? <StudioIcon name="pin" className="sesspin-marker" aria-label={t("已置顶")} /> : null;
  return <button className="sesspin-action" data-action="session.pin" data-target={target}
    aria-pressed={pinned} disabled={disabled}
    title={t(pinned ? "取消置顶" : "置顶会话")}
    aria-label={t(pinned ? "取消置顶：{title}" : "置顶会话：{title}", { title })}
    onClick={(event) => { event.stopPropagation(); onPin(); }}>
    <StudioIcon name="pin" aria-label={pinned ? t("已置顶") : undefined} />
  </button>;
}

export function SessionArchiveAction({ target, title, archived, disabled, onArchive }: Target & {
  archived: boolean;
  onArchive: () => void;
}) {
  return <span className="sessquick"><button data-action="session.archive" data-target={target}
    data-value={archived ? "restore" : "archive"} disabled={disabled}
    title={t(archived ? "取消归档" : "归档会话")}
    aria-label={t(archived ? "取消归档：{title}" : "归档会话：{title}", { title })}
    onClick={(event) => { event.stopPropagation(); onArchive(); }}>
    <StudioIcon name="archive" />
  </button></span>;
}
