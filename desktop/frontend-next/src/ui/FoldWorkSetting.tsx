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
    <Group id="fold-work" title={t("settings.fold_the_work_in_between")} hint={t("settings.when_a_turn_ends_tool")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("settings.fold_the_work_inside_a")}</span>
          <span className="ds">{t("settings.off_by_default_a_step")}</span>
        </span>
        <Switch data-action="chrome.fold-work" on={on} label={t("settings.fold_the_work_inside_a")} onClick={() => {
          setError("");
          saveFoldWorkPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
