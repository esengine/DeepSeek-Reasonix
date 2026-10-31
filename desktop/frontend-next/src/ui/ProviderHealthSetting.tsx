import { useEffect, useRef, useState } from "react";
import { HttpError, type AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { Switch } from "./Switch";
import { Group } from "./Group";

export function ProviderHealthSetting({ port, onChanged, onFailed }: {
  port: Partial<Pick<AgentPort, "providerHealthSettings" | "saveProviderHealthSettings">>;
  onChanged: () => void; onFailed: (why: string) => void;
}) {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState<"unsupported" | "failed" | null>(null);
  const generation = useRef(0);
  useEffect(() => {
    let live = true;
    generation.current++;
    setEnabled(null);
    setBusy(false);
    setUnavailable(null);
    if (!port.providerHealthSettings || !port.saveProviderHealthSettings) setUnavailable("unsupported");
    else void port.providerHealthSettings().then((value) => { if (live) setEnabled(value.enabled); }).catch((e) => {
      if (live) setUnavailable(e instanceof HttpError && [404, 405, 501].includes(e.status) ? "unsupported" : "failed");
    });
    return () => { live = false; generation.current++; };
  }, [port]);
  return <Group id="provider-health" title={t("自动检测连接与余额")}
    hint={t("默认关闭。开启后，打开模型列表会使用已保存密钥检查已授权的全局连接和余额，不发送生成请求。")}
    action={<Switch data-action="provider.health-setting" on={enabled === true}
      busy={busy || enabled === null || !port.saveProviderHealthSettings}
      label={t("自动检测连接与余额")} onClick={async () => {
        if (!port.saveProviderHealthSettings) return;
        const version = generation.current;
        setBusy(true);
        try {
          const value = await port.saveProviderHealthSettings(!enabled);
          if (version === generation.current) { setEnabled(value.enabled); onChanged(); }
        } catch (e) { if (version === generation.current) onFailed(reason(e)); }
        finally { if (version === generation.current) setBusy(false); }
      }} />}>
    {unavailable && <p className="note" role="status">{t(unavailable === "unsupported"
      ? "当前服务器不支持自动检测，请升级服务器后重试。"
      : "无法读取检测设置，请检查连接或权限后重试。")}</p>}
  </Group>;
}
