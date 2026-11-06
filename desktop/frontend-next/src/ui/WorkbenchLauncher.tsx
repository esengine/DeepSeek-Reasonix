import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

/** A new tab: the three things the workbench opens, then what was opened lately. */
export function WorkbenchLauncher({ changes, focus = false, onFiles, onFind, onBrowser, onTerminal, recent = [], onOpenFile }: { changes: number; focus?: boolean; onFiles: () => void; onFind: () => void; onBrowser: () => void; onTerminal?: () => void; recent?: string[]; onOpenFile?: (path: string) => void }) {
  return (
    <div className="workbench-launcher" role="group" aria-label={t("common.start")}>
      <p className="workbench-launcher-title">{t("workbench.open_in_the_workbench")}</p>
      <button autoFocus={focus} aria-label={t("workbench.explorer")} data-action="workbench.launch-files" onClick={onFiles}>
        <StudioIcon name="folder" />
        <span><b>{t("workbench.explorer")}{changes > 0 && <small>{changes}</small>}</b><i>{t("workbench.changes_history_and_files")}</i></span>
      </button>
      <button aria-label={t("workbench.find_a_file")} data-action="workbench.launch-find" onClick={onFind}>
        <StudioIcon name="file" />
        <span><b>{t("workbench.find_a_file")}</b><i>{t("workbench.find_a_file_by_name")}</i></span>
      </button>
      <button aria-label={t("workbench.browser")} data-action="workbench.launch-browser" onClick={onBrowser}>
        <StudioIcon name="globe" />
        <span><b>{t("workbench.browser")}</b><i>{t("workbench.view_web_pages_beside_the")}</i></span>
      </button>
      {onTerminal && (
        <button aria-label={t("terminal.name")} data-action="workbench.launch-terminal" onClick={onTerminal}>
          <StudioIcon name="terminal" />
          <span><b>{t("terminal.name")}</b><i>{t("terminal.launch_hint")}</i></span>
        </button>
      )}
      {recent.length > 0 && onOpenFile && (
        <div className="workbench-launcher-recent" role="group" aria-label={t("workbench.recently_opened")}>
          <p>{t("workbench.recently_opened")}</p>
          {recent.map((path) => (
            <button key={path} data-action="workbench.launch-recent" data-target={path} title={path} onClick={() => onOpenFile(path)}>
              <StudioIcon name="file" />
              <span>{path.slice(path.lastIndexOf("/") + 1)}</span>
              <i>{path.includes("/") ? path.slice(0, path.lastIndexOf("/") + 1) : ""}</i>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
