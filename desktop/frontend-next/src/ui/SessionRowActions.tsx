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
  if (!enabled) return pinned ? <StudioIcon name="pin" className="sesspin-marker" aria-label={t("sessionRowActions.pinned")} /> : null;
  return <button className="sesspin-action" data-action="session.pin" data-target={target}
    aria-pressed={pinned} disabled={disabled}
    title={t(pinned ? "common.unpin" : "common.pinSession")}
    aria-label={t(pinned ? "sessionRowActions.unpinSession" : "sessionRowActions.pinSession", { title })}
    onClick={(event) => { event.stopPropagation(); onPin(); }}>
    <StudioIcon name="pin" aria-label={pinned ? t("sessionRowActions.pinned") : undefined} />
  </button>;
}

export function SessionArchiveAction({ target, title, archived, disabled, onArchive }: Target & {
  archived: boolean;
  onArchive: () => void;
}) {
  return <span className="sessquick"><button data-action="session.archive" data-target={target}
    data-value={archived ? "restore" : "archive"} disabled={disabled}
    title={t(archived ? "common.unarchive" : "common.archiveSession")}
    aria-label={t(archived ? "sessionRowActions.unarchive" : "sessionRowActions.archiveSession", { title })}
    onClick={(event) => { event.stopPropagation(); onArchive(); }}>
    <StudioIcon name="archive" />
  </button></span>;
}
