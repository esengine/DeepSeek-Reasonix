import { t } from "../i18n";
import { setRunningMessageIntent, useRunningMessageIntent } from "../state/prefs";
import { Group } from "./Group";

export function RunningMessageSetting() {
  const intent = useRunningMessageIntent();
  return (
    <Group id="running-message-intent" title={t("运行中发送消息")} hint={t("选择运行中发送消息的默认方式。仅保存在此设备，不改变已排队的消息。")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("运行中发送消息")}>
        <button role="radio" data-action="composer.running-message-intent" data-value="steer" aria-checked={intent === "steer"} onClick={() => setRunningMessageIntent("steer")}>{t("下个工具边界插话")}</button>
        <button role="radio" data-action="composer.running-message-intent" data-value="followup" aria-checked={intent === "followup"} onClick={() => setRunningMessageIntent("followup")}>{t("整轮结束后发送")}</button>
      </div>
      <p className="hint">{t("可在待发送队列中选择“立即发送”来停止当前回复并开始新一轮。暂停、编辑和取回仍然可用。")}</p>
    </Group>
  );
}
