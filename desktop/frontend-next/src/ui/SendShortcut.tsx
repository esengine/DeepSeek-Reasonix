import { t } from "../i18n";
import { setSendShortcut, useSendShortcut } from "../state/prefs";
import { acceleratorLabel } from "./keys";
import { SegChoice, SettingRow } from "./SettingRow";

type Mode = "enter" | "modifier_enter";

export function SendShortcut() {
  const mode = useSendShortcut();
  return (
    <SettingRow id="send-shortcut" title={t("composer.send_shortcut")} desc={t("composer.send_with_enter_or_with")}>
      <SegChoice<Mode> label={t("composer.send_shortcut")} value={mode} action="composer.send-shortcut" onPick={setSendShortcut}
        options={[["enter", "Enter"], ["modifier_enter", acceleratorLabel("CommandOrControl+Enter")]]} />
    </SettingRow>
  );
}
