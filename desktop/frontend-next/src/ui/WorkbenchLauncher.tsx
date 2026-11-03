import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function WorkbenchLauncher({ changes, focus = false, onFiles, onChanges, onBrowser }: { changes: number; focus?: boolean; onFiles: () => void; onChanges: () => void; onBrowser: () => void }) {
  return (
    <div className="workbench-launcher" role="group" aria-label={t("开始")}>
      <StudioIcon name="spark" />
      <p className="workbench-launcher-title">{t("在工作台打开")}</p>
      <button autoFocus={focus} aria-label={t("文件")} data-action="workbench.launch-files" onClick={onFiles}>
        <StudioIcon name="folder" />
        <span><b>{t("文件")}</b><i>{t("资源管理器：改动、提交历史和文件")}</i></span>
      </button>
      <button aria-label={t("改动")} data-action="workbench.launch-changes" disabled={changes === 0} onClick={onChanges}>
        <StudioIcon name="branch" />
        <span><b>{t("改动")}</b><i>{t("这次会话改了哪些文件")}</i></span>
        {changes > 0 && <small>{changes}</small>}
      </button>
      <button aria-label={t("浏览器")} data-action="workbench.launch-browser" onClick={onBrowser}>
        <StudioIcon name="globe" />
        <span><b>{t("浏览器")}</b><i>{t("和对话并排看网页")}</i></span>
      </button>
    </div>
  );
}
