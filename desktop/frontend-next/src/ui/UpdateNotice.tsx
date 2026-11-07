import { t } from "../i18n";
import { useUpdateAvailable, useUpdateReady } from "../state/updatewatch";

export function UpdateNotice({ onOpen }: { onOpen: () => void }) {
  const available = useUpdateAvailable();
  const ready = useUpdateReady();
  const version = ready || available;
  if (!version) return null;
  return (
    <button className="studio-update" data-action="update.open" onClick={onOpen}>
      <span aria-hidden="true">
        <svg viewBox="0 0 16 16" width={16} height={16} fill="none" stroke="currentColor" strokeWidth={1.45} strokeLinecap="round" strokeLinejoin="round"><path d="M8 2.4v7.4M5.2 7.2 8 10l2.8-2.8M3 12.8h10" /></svg>
      </span>
      <b>{t(ready ? "about.version_v_is_downloaded_short" : "about.version_v_is_available_short", { v: version })}</b>
    </button>
  );
}
