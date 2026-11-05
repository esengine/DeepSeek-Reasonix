import { useEffect, useRef, useState } from "react";
import { HttpError, type AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { Switch } from "./Switch";
import { SettingRow } from "./SettingRow";

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
  return <>
    <SettingRow id="provider-health" title={t("models.automatically_check_connections_and_balances")} desc={t("models.check_while_the_list_opens")}>
      <Switch data-action="provider.health-setting" on={enabled === true}
        busy={busy || enabled === null || !port.saveProviderHealthSettings}
        label={t("models.automatically_check_connections_and_balances")} onClick={async () => {
          if (!port.saveProviderHealthSettings) return;
          const version = generation.current;
          setBusy(true);
          try {
            const value = await port.saveProviderHealthSettings(!enabled);
            if (version === generation.current) { setEnabled(value.enabled); onChanged(); }
          } catch (e) { if (version === generation.current) onFailed(reason(e)); }
          finally { if (version === generation.current) setBusy(false); }
        }} />
    </SettingRow>
    {unavailable && <p className="note" role="status">{t(unavailable === "unsupported"
      ? "models.this_server_does_not_support"
      : "models.cannot_read_check_settings_check")}</p>}
  </>;
}
