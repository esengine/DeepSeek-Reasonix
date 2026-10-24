import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PresentFilesSettings } from "../port/port";
import { Switch } from "./Switch";

export function PresentFiles({ port }: { port: AgentPort }) {
  const [state, setState] = useState<PresentFilesSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let current = true;
    setState(null);
    setError("");
    port.presentFiles().then((value) => { if (current) setState(value); })
      .catch((e: unknown) => { if (current) setError(reason(e)); });
    return () => { current = false; };
  }, [port]);

  const flip = async () => {
    if (!state || busy) return;
    setBusy(true);
    setError("");
    try {
      setState(await port.savePresentFiles(!state.enabled));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="box">
      {state ? (
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("启用 AI 文件交付卡片")}</span>
            <span className="ds">{t("允许 AI 将已生成的文件展示为可点击的卡片，在工作台中打开预览。仅保存到用户配置，重新打开会话后生效。")}</span>
          </span>
          <Switch data-action="present-files.enabled" on={state.enabled} busy={busy}
            label={t("启用 AI 文件交付卡片")} onClick={() => void flip()} />
        </div>
      ) : <div className="empty">{t(error ? "无法读取文件交付设置。" : "加载中…")}</div>}
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
