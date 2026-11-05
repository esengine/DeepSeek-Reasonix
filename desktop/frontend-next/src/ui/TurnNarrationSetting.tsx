import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { TurnNarrationSettings } from "../port/boundary";
import type { AgentPort } from "../port/port";
import { SegChoice, SettingRow } from "./SettingRow";

type Level = TurnNarrationSettings["level"];

export function TurnNarrationSetting({ port }: { port: AgentPort }) {
  const [level, setLevel] = useState<Level | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    port.turnNarrationSettings().then((s) => { if (alive) setLevel(s.level); }).catch((e) => { if (alive) setError(reason(e)); });
    return () => { alive = false; };
  }, [port]);
  const choose = (next: Level) => {
    setError("");
    port.saveTurnNarrationSettings(next).then((s) => setLevel(s.level)).catch((e) => setError(reason(e)));
  };
  return (
    <>
      <SettingRow id="turn-narration" title={t("settings.narration_between_tool_calls")} desc={t("settings.say_what_a_step_did")}>
        <SegChoice<Level> label={t("settings.narration_between_tool_calls")} value={level ?? "brief"} action="chrome.turn-narration" disabled={level === null}
          onPick={choose} options={[["brief", t("settings.brief")], ["detailed", t("settings.detailed")]]} />
      </SettingRow>
      {error && <div className="why">{error}</div>}
    </>
  );
}
