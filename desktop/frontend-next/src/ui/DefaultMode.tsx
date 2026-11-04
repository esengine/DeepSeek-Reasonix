import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, DefaultModeSettings } from "../port/port";
import { SegChoice, SettingRow } from "./SettingRow";

type Start = "agent" | "plan";

export function DefaultMode({ port }: { port: AgentPort }) {
  const [state, setState] = useState<DefaultModeSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .defaultMode()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  const row = (body: React.ReactNode) => (
    <SettingRow id="default-mode" title={t("safety.how_new_conversations_start")} desc={t("safety.which_mode_a_new_chat")}>
      {body}
    </SettingRow>
  );

  if (!state) return row(<span className="srow-desc">{t("safety.could_not_read_the_setting")}</span>);

  const pick = async (next: Start) => {
    if ((next === "plan") === state.plan) return;
    setBusy(true);
    setError("");
    try {
      setState(await port.saveDefaultMode(next === "plan"));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return row(
    <>
      <SegChoice<Start> label={t("safety.how_new_conversations_start")} value={state.plan ? "plan" : "agent"} disabled={busy} action="default-mode.plan" onPick={(next) => void pick(next)}
        options={[["agent", t("Agent")], ["plan", t("common.plan")]]} />
      {error && <span className="why">{error}</span>}
    </>,
  );
}
