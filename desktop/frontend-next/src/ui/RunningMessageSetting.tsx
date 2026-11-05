import { t } from "../i18n";
import { setRunningMessageIntent, useRunningMessageIntent } from "../state/prefs";
import { Group } from "./Group";

export function RunningMessageSetting() {
  const intent = useRunningMessageIntent();
  return (
    <Group id="running-message-intent" title={t("settings.messages_sent_while_running")} hint={t("settings.what_a_message_sent_while_running_does")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("settings.messages_sent_while_running")}>
        <button role="radio" data-action="composer.running-message-intent" data-value="steer" aria-checked={intent === "steer"} onClick={() => setRunningMessageIntent("steer")}>{t("settings.steer_at_the_next_tool")}</button>
        <button role="radio" data-action="composer.running-message-intent" data-value="followup" aria-checked={intent === "followup"} onClick={() => setRunningMessageIntent("followup")}>{t("settings.send_after_the_whole_run")}</button>
      </div>
      <p className="hint">{t("settings.use_send_now_in_the")}</p>
    </Group>
  );
}
