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
    <Group id="fold-work" title={t("common.foldTheWorkInBetween")} hint={t("foldWorkSetting.whenATurnEndsTool")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("foldWorkSetting.foldTheWorkInsideA")}</span>
          <span className="ds">{t("foldWorkSetting.offByDefaultAStep")}</span>
        </span>
        <Switch data-action="chrome.fold-work" on={on} label={t("foldWorkSetting.foldTheWorkInsideA")} onClick={() => {
          setError("");
          saveFoldWorkPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
