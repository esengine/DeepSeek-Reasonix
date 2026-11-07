import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { HubPort } from "../port/hub";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { StudioIcon } from "./StudioIcon";

export function NativeSessionImport({ hub, workspace, onImported }: { hub: HubPort; workspace: string; onImported: () => void }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const [asking, setAsking] = useState(false);
  const [over, setOver] = useState(false);
  const take = (file: File | undefined) => {
    if (!file) return;
    setAsking(false);
    setBusy(true); setError(""); setMessage("");
    void (async () => {
      await hub.importNativeSession(file, workspace);
      setMessage(t("data.session_imported_with_its_original"));
      onImported();
    })().catch((e) => setError(reason(e))).finally(() => setBusy(false));
  };
  return <section className="grp">
    <h3 className="lbl">{t("data.import_a_native_session")}</h3>
    <div className="item">
      <div className="l">
        <b>{t("data.import_from_a_session_file")}</b>
        <span className="hint">{t("data.keep_the_composition_version_and")}</span>
      </div>
      <input ref={input} hidden type="file" accept="application/json,.json" aria-label={t("data.choose_a_session_json_file")} data-action-change="session.import-native" disabled={busy || !workspace} onChange={(event) => {
        take(event.target.files?.[0]);
        event.target.value = "";
      }} />
      <button className="btn" data-action="session.import-native" disabled={busy || !workspace} onClick={() => setAsking(true)}>
        {t(busy ? "common.importing" : "data.choose_a_session_file")}
      </button>
    </div>
    {message && <div className="ns-done" role="status"><StudioIcon name="check" />{message}</div>}
    {error && <div className="ns-fail" role="alert"><StudioIcon name="warning" />{error}</div>}
    {asking && createPortal(
      <div className="ns-veil" data-action-mousedown="layer.dismiss" onMouseDown={(e) => { if (e.target === e.currentTarget) setAsking(false); }}>
        <div className="ns-dlg" role="dialog" aria-modal="true" aria-labelledby="ns-title" data-action-keydown="layer.dismiss"
          onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); setAsking(false); } }}>
          <header>
            <h3 id="ns-title">{t("data.import_a_native_session")}</h3>
            <button type="button" className="ns-x" data-action="layer.dismiss" aria-label={t("common.close")} title={t("common.close")} onClick={() => setAsking(false)}><StudioIcon name="close" /></button>
          </header>
          <p>{t("data.keep_the_composition_version_and")}</p>
          <div className="ns-drop" data-over={over ? "" : undefined} data-action-dragover="session.import-native-choose" data-action-dragleave="session.import-native-choose" data-action-drop="session.import-native-choose"
            onDragOver={(e) => { e.preventDefault(); setOver(true); }}
            onDragLeave={() => setOver(false)}
            onDrop={(e) => { e.preventDefault(); setOver(false); take(e.dataTransfer.files?.[0]); }}>
            <StudioIcon name="file" />
            <span>{t("data.drop_a_session_json_file_here")}</span>
            <button type="button" className="btn" autoFocus data-action="session.import-native-choose" onClick={() => input.current?.click()}>{t("data.choose_a_session_json_file")}</button>
          </div>
          <footer><button type="button" className="ns-cancel" data-action="layer.dismiss" onClick={() => setAsking(false)}>{t("common.cancel")}</button></footer>
        </div>
      </div>,
      document.body,
    )}
  </section>;
}
