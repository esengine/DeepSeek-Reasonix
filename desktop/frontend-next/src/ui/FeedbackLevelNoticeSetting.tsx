import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { loadLevelNoticePref, saveLevelNoticePref, useShowsLevelNotice } from "../state/levelnotices";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function FeedbackLevelNoticeSetting({ port }: { port: AgentPort }) {
  const on = useShowsLevelNotice(port);
  const [error, setError] = useState("");
  useEffect(() => {
    void loadLevelNoticePref(port);
  }, [port]);
  return (
    <Group id="feedback-level-notices" inline title={t("feedback.level_notices")} hint={t("settings.notice_when_feedback_adopted")}>
      <Switch data-action="chrome.feedback-level-notices" on={on} label={t("feedback.tell_me_when_my_level")} onClick={() => {
          setError("");
          saveLevelNoticePref(port, !on).catch((e) => setError(reason(e)));
        }} />
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
