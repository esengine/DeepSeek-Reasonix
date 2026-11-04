import { t } from "../i18n";
import { setSendShortcut, useSendShortcut } from "../state/prefs";
import { chord } from "./keys";
import { SegChoice, SettingRow } from "./SettingRow";

type Mode = "enter" | "modifier_enter";

export function SendShortcut() {
  const mode = useSendShortcut();
  return (
    <SettingRow id="send-shortcut" title={t("发送快捷键")} desc={t("Enter 发送还是 Ctrl+Enter 发送")}>
      <SegChoice<Mode> label={t("发送快捷键")} value={mode} action="composer.send-shortcut" onPick={setSendShortcut}
        options={[["enter", "Enter"], ["modifier_enter", chord("Enter")]]} />
    </SettingRow>
  );
}
