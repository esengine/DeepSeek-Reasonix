import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BrowserToolsSettings } from "../port/port";
import { SettingRow } from "./SettingRow";
import { Switch } from "./Switch";

// One switch over whether the browser tools are bound at all. The row reads the
// user file; a project file that sets the same key outranks it, and then the
// value this workspace runs with is said beside it.
export function BrowserTools({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<BrowserToolsSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .browserTools()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("workbench.could_not_read_the_built")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveBrowserTools(!state.enabled));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <SettingRow id="browser-tools" title={t("common.built_in_browser")} desc={t("workbench.let_it_open_pages_in_the_browser")}>
        <Switch data-action="browser-tools.enabled" on={state.enabled} busy={busy} label={t("workbench.enable_the_built_in_browser")} onClick={() => void flip()} />
      </SettingRow>
      {state.effective !== state.enabled && (
        <p className="note">
          {t("common.actually_in_force")} · {t(state.effective ? "workbench.this_projects_config_file_turns_2" : "workbench.this_projects_config_file_turns")}
        </p>
      )}
      {error && <div className="why">{error}</div>}
    </>
  );
}
