import { t } from "../i18n";
import { useUpdateAvailable } from "../state/updatewatch";

export function UpdateDot() {
  const version = useUpdateAvailable();
  if (!version) return null;
  const says = t("updateDot.versionIsAvailableSeeSettings", { v: version });
  return (
    <>
      <i className="upd-dot" aria-hidden="true" title={says} />
      <span className="sr-only">{says}</span>
    </>
  );
}
