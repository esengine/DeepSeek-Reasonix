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
    <Group id="changed-files" inline title={t("settings.file_changes")} hint={t("settings.show_n_files_changed_above_input")}>
      <Switch data-action="chrome.changed-files" on={on} label={t("settings.show_file_changes_above_the")} onClick={() => {
          setError("");
          saveChangedFilesPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
