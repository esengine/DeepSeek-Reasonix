import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { saveChangedFilesPref, useShowsChangedFiles } from "../state/changedfiles";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function ChangedFilesSetting({ port }: { port: AgentPort }) {
  const on = useShowsChangedFiles(port);
  const [error, setError] = useState("");
  return (
    <Group id="changed-files" title={t("common.fileChanges")} hint={t("changedFilesSetting.inAGitWorkspaceShow")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("changedFilesSetting.showFileChangesAboveThe")}</span>
          <span className="ds">{t("changedFilesSetting.onByDefaultTheCount")}</span>
        </span>
        <Switch data-action="chrome.changed-files" on={on} label={t("changedFilesSetting.showFileChangesAboveThe")} onClick={() => {
          setError("");
          saveChangedFilesPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
