import { useState } from "react";
import type { HubPort } from "../port/hub";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

export function NativeSessionImport({ hub, workspace, onImported }: { hub: HubPort; workspace: string; onImported: () => void }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  return <section className="grp">
    <h3 className="lbl">{t("导入原生会话")}</h3>
    <p className="hint">{t("保留组合版本与资源；导入不会安装程序或授予权限。缺少授权的会话仅可查看历史。")}</p>
    <label>{t("选择会话 JSON 文件")}<input type="file" accept="application/json,.json" data-action-change="session.import-native" disabled={busy || !workspace} onChange={(event) => {
      const file = event.target.files?.[0];
      if (!file) return;
      setBusy(true); setError(""); setMessage("");
      void (async () => {
        if (file.size > 192 * 1024 * 1024) throw new Error(t("会话文件超过大小限制。"));
        await hub.importNativeSession(JSON.parse(await file.text()), workspace);
        setMessage(t("会话已导入，绑定版本保持不变。"));
        onImported();
      })().catch((e) => setError(reason(e))).finally(() => setBusy(false));
    }} /></label>
    {message && <p role="status">{message}</p>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
