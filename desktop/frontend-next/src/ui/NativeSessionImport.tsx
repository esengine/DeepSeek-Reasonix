import { useRef, useState } from "react";
import type { HubPort } from "../port/hub";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

export function NativeSessionImport({ hub, workspace, onImported }: { hub: HubPort; workspace: string; onImported: () => void }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  return <section className="grp">
    <h3 className="lbl">{t("nativeSessionImport.importANativeSession")}</h3>
    <div className="item">
      <div className="l">
        <b>{t("nativeSessionImport.importFromASessionFile")}</b>
        <span className="hint">{t("nativeSessionImport.keepTheCompositionVersionAnd")}</span>
        {message && <span className="ready" role="status">{message}</span>}
        {error && <span className="held" role="alert">{error}</span>}
      </div>
      <input ref={input} hidden type="file" accept="application/json,.json" aria-label={t("nativeSessionImport.chooseASessionJSONFile")} data-action-change="session.import-native" disabled={busy || !workspace} onChange={(event) => {
        const file = event.target.files?.[0];
        if (!file) return;
        setBusy(true); setError(""); setMessage("");
        void (async () => {
          await hub.importNativeSession(file, workspace);
          setMessage(t("nativeSessionImport.sessionImportedWithItsOriginal"));
          onImported();
        })().catch((e) => setError(reason(e))).finally(() => setBusy(false));
        event.target.value = "";
      }} />
      <button className="btn" data-action="session.import-native" disabled={busy || !workspace} onClick={() => input.current?.click()}>
        {t(busy ? "common.importing" : "nativeSessionImport.chooseASessionFile")}
      </button>
    </div>
  </section>;
}
