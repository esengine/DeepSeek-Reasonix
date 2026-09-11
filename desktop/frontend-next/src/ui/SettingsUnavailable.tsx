import { t } from "../i18n";

// What is shown when the settings chunk does not arrive. An update replaces the
// assets under a window still holding the old document, so the name it asks for
// is gone; the rejected import throws during render, and without a boundary
// that unmounts the whole tree — a white window, over a panel that did not open.
export function SettingsUnavailable({ onClose }: { onClose: () => void }) {
  return (
    <div className="prefs">
      <div className="prefs-sheet" role="alertdialog" aria-modal="true" aria-labelledby="prefs-gone">
        <div className="prefs-hd">
          <h1 id="prefs-gone">{t("设置没能打开")}</h1>
          <button className="btn sm" data-action="chrome.reload" onClick={() => location.reload()}>
            {t("重新载入")}
          </button>
          <button className="btn sm" data-action="settings.close" onClick={onClose}>
            {t("关闭")}
          </button>
        </div>
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("这一页是单独取的，这次没取到")}</span>
          <span className="why">
            {t("多半是更新换掉了这个窗口正在用的那份文件。重新载入窗口即可，当前会话与正在跑的回合都不受影响。")}
          </span>
        </div>
      </div>
    </div>
  );
}
