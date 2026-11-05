import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { saveFoldWorkPref, useFoldsTurnWork } from "../state/foldwork";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function FoldWorkSetting({ port }: { port: AgentPort }) {
  const on = useFoldsTurnWork(port);
  const [error, setError] = useState("");
  return (
    <Group id="fold-work" inline title={t("settings.fold_the_work_in_between")} hint={t("settings.tool_calls_and_thinking_fold")}>
      <Switch data-action="chrome.fold-work" on={on} label={t("settings.fold_the_work_inside_a")} onClick={() => {
          setError("");
          saveFoldWorkPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
