import { useRef } from "react";
import { useModalFocus } from "./useModalFocus";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

// What is shown when the settings chunk does not arrive. An update replaces the
// assets under a window still holding the old document, so the name it asks for
// is gone; the rejected import throws during render, and without a boundary
// that unmounts the whole tree — a white window, over a panel that did not open.
export function SettingsUnavailable({ onClose }: { onClose: () => void }) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  return (
    <div className="prefs">
      <div ref={sheet} className="prefs-sheet gone" role="alertdialog" aria-modal="true" aria-labelledby="prefs-gone">
        <span className="gone-icon" aria-hidden="true"><StudioIcon name="warning" /></span>
        <h1 id="prefs-gone">{t("settings.settings_did_not_open")}</h1>
        <p className="gone-t" role="alert">{t("settings.this_screen_is_fetched_on")}</p>
        <p className="gone-why">{t("settings.an_update_most_likely_replaced")}</p>
        <div className="gone-acts">
          <button className="btn ghost" data-action="settings.close" onClick={onClose}>{t("common.close")}</button>
          <button className="btn" data-primary="" data-action="chrome.reload" onClick={() => location.reload()}><StudioIcon name="refresh" />{t("settings.reload")}</button>
        </div>
      </div>
    </div>
  );
}
