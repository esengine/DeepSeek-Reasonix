import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { TurnNarrationSettings } from "../port/boundary";
import type { AgentPort } from "../port/port";
import { Group } from "./Group";

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
    <Group id="turn-narration" title={t("common.narrationBetweenToolCalls")} hint={t("turnNarrationSetting.howMuchTheModelSays")}>
      <div className="seg" data-text role="group" aria-label={t("common.narrationBetweenToolCalls")}>
        <button data-action="chrome.turn-narration" data-value="brief" aria-pressed={level === "brief"} disabled={level === null} onClick={() => choose("brief")}>{t("common.brief")}</button>
        <button data-action="chrome.turn-narration" data-value="detailed" aria-pressed={level === "detailed"} disabled={level === null} onClick={() => choose("detailed")}>{t("common.detailed")}</button>
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
