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
    <h3 className="lbl">{t("导入原生会话")}</h3>
    <div className="item">
      <div className="l">
        <b>{t("从会话文件导入")}</b>
        <span className="hint">{t("保留组合版本与资源；导入不会安装程序或授予权限。缺少授权的会话仅可查看历史。")}</span>
        {message && <span className="ready" role="status">{message}</span>}
        {error && <span className="held" role="alert">{error}</span>}
      </div>
      <input ref={input} hidden type="file" accept="application/json,.json" aria-label={t("选择会话 JSON 文件")} data-action-change="session.import-native" disabled={busy || !workspace} onChange={(event) => {
        const file = event.target.files?.[0];
        if (!file) return;
        setBusy(true); setError(""); setMessage("");
        void (async () => {
          await hub.importNativeSession(file, workspace);
          setMessage(t("会话已导入，绑定版本保持不变。"));
          onImported();
        })().catch((e) => setError(reason(e))).finally(() => setBusy(false));
        event.target.value = "";
      }} />
      <button className="btn" data-action="session.import-native" disabled={busy || !workspace} onClick={() => input.current?.click()}>
        {t(busy ? "正在导入…" : "选择会话文件…")}
      </button>
    </div>
  </section>;
}
