import { useRef } from "react";
import { useModalFocus } from "./useModalFocus";
import { t } from "../i18n";

// What is shown when the settings chunk does not arrive. An update replaces the
// assets under a window still holding the old document, so the name it asks for
// is gone; the rejected import throws during render, and without a boundary
// that unmounts the whole tree — a white window, over a panel that did not open.
export function SettingsUnavailable({ onClose }: { onClose: () => void }) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  return (
    <div className="prefs">
      <div ref={sheet} className="prefs-sheet" role="alertdialog" aria-modal="true" aria-labelledby="prefs-gone">
        <div className="prefs-hd">
          <h1 id="prefs-gone">{t("settings.settings_did_not_open")}</h1>
          <button className="btn sm" data-action="chrome.reload" onClick={() => location.reload()}>
            {t("settings.reload")}
          </button>
          <button className="btn sm" data-action="settings.close" onClick={onClose}>
            {t("common.close")}
          </button>
        </div>
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("settings.this_screen_is_fetched_on")}</span>
          <span className="why">
            {t("settings.an_update_most_likely_replaced")}
          </span>
        </div>
      </div>
    </div>
  );
}
