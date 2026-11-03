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
    <Group id="feedback-level-notices" title={t("common.levelNotices")} hint={t("feedbackLevelNoticeSetting.whenYourFeedbackShipsAnd")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("feedbackLevelNoticeSetting.tellMeWhenMyLevel")}</span>
          <span className="ds">{t("feedbackLevelNoticeSetting.noPopUpAndNo")}</span>
        </span>
        <Switch data-action="chrome.feedback-level-notices" on={on} label={t("feedbackLevelNoticeSetting.tellMeWhenMyLevel")} onClick={() => {
          setError("");
          saveLevelNoticePref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
