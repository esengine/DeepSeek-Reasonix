import { StudioIcon } from "./StudioIcon";
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
        <StudioIcon name="download" />
      </span>
      <b>{t(ready ? "about.version_v_is_downloaded_short" : "about.version_v_is_available_short", { v: version })}</b>
    </button>
  );
}
