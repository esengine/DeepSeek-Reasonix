import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function WorkbenchLauncher({ changes, focus = false, onFiles, onChanges, onBrowser }: { changes: number; focus?: boolean; onFiles: () => void; onChanges: () => void; onBrowser: () => void }) {
  return (
    <div className="workbench-launcher" role="group" aria-label={t("common.start")}>
      <StudioIcon name="spark" />
      <button autoFocus={focus} data-action="workbench.launch-files" onClick={onFiles}>
        <StudioIcon name="folder" />
        <b>{t("common.files2")}</b>
      </button>
      <button data-action="workbench.launch-changes" disabled={changes === 0} onClick={onChanges}>
        <StudioIcon name="branch" />
        <b>{t("common.changes")}</b>
        {changes > 0 && <small>{changes}</small>}
      </button>
      <button data-action="workbench.launch-browser" onClick={onBrowser}>
        <StudioIcon name="globe" />
        <b>{t("common.browser")}</b>
      </button>
    </div>
  );
}
