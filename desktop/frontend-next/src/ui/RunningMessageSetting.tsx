import { t } from "../i18n";
import { setRunningMessageIntent, useRunningMessageIntent } from "../state/prefs";
import { Group } from "./Group";

export function RunningMessageSetting() {
  const intent = useRunningMessageIntent();
  return (
    <Group id="running-message-intent" title={t("common.messagesSentWhileRunning")} hint={t("runningMessageSetting.chooseTheDefaultForMessages")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("common.messagesSentWhileRunning")}>
        <button role="radio" data-action="composer.running-message-intent" data-value="steer" aria-checked={intent === "steer"} onClick={() => setRunningMessageIntent("steer")}>{t("runningMessageSetting.steerAtTheNextTool")}</button>
        <button role="radio" data-action="composer.running-message-intent" data-value="followup" aria-checked={intent === "followup"} onClick={() => setRunningMessageIntent("followup")}>{t("runningMessageSetting.sendAfterTheWholeRun")}</button>
      </div>
      <p className="hint">{t("runningMessageSetting.useSendNowInThe")}</p>
    </Group>
  );
}
