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
            <span className="lb">{t("workbench.enable_ai_file_delivery_cards")}</span>
            <span className="ds">{t("workbench.let_ai_present_generated_files")}</span>
          </span>
          <Switch data-action="present-files.enabled" on={state.enabled} busy={busy}
            label={t("workbench.enable_ai_file_delivery_cards")} onClick={() => void flip()} />
        </div>
      ) : <div className="empty">{t(error ? "workbench.could_not_read_file_delivery" : "workbench.loading")}</div>}
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
