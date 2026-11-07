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
  const marker = pinned ? <StudioIcon name="pin" className="sesspin-marker ds-pin" aria-label={enabled ? undefined : t("session.pinned")} aria-hidden={enabled ? true : undefined} /> : null;
  if (!enabled) return marker;
  return <>
    {marker}
    <button className="sesspin-action" data-action="session.pin" data-target={target}
      aria-pressed={pinned} disabled={disabled}
      title={t(pinned ? "common.unpin" : "common.pin_session")}
      aria-label={t(pinned ? "session.unpin_session_title" : "session.pin_session_title", { title })}
      onClick={(event) => { event.stopPropagation(); onPin(); }}>
      <StudioIcon name="pin" aria-label={pinned ? t("session.pinned") : undefined} />
    </button>
  </>;
}

export function SessionArchiveAction({ target, title, archived, disabled, onArchive }: Target & {
  archived: boolean;
  onArchive: () => void;
}) {
  return <span className="sessquick"><button data-action="session.archive" data-target={target}
    data-value={archived ? "restore" : "archive"} disabled={disabled}
    title={t(archived ? "common.unarchive" : "common.archive_session")}
    aria-label={t(archived ? "session.unarchive_title" : "session.archive_session_title", { title })}
    onClick={(event) => { event.stopPropagation(); onArchive(); }}>
    <StudioIcon name="archive" />
  </button></span>;
}

export function SessionMoreAction({ target, disabled, onOpen }: { target: string; disabled?: boolean; onOpen: (trigger: HTMLElement) => void }) {
  return <button className="ds-more" data-action="session.menu" data-target={target} aria-haspopup="menu" disabled={disabled}
    title={t("common.session_actions")} aria-label={t("common.session_actions")}
    onClick={(event) => { event.stopPropagation(); onOpen(event.currentTarget.closest<HTMLElement>(".sessrow") ?? event.currentTarget); }}>
    <StudioIcon name="more" />
  </button>;
}
