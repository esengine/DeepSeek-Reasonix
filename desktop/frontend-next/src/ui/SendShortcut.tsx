import { t } from "../i18n";
import { setSendShortcut, useSendShortcut } from "../state/prefs";
import { Group } from "./Group";
import { chord } from "./keys";

export function SendShortcut() {
  const mode = useSendShortcut();
  return (
    <Group id="send-shortcut" title={t("common.sendShortcut")} hint={t("sendShortcut.appliesToTaskInputEditing")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("common.sendShortcut")}>
        <button role="radio" data-action="composer.send-shortcut" data-value="enter" aria-checked={mode === "enter"} onClick={() => setSendShortcut("enter")}>{t("sendShortcut.enterToSend")}</button>
        <button role="radio" data-action="composer.send-shortcut" data-value="modifier_enter" aria-checked={mode === "modifier_enter"} onClick={() => setSendShortcut("modifier_enter")}>{t("sendShortcut.toSend", { key: chord("Enter") })}</button>
      </div>
      <p className="hint">{mode === "enter" ? t("sendShortcut.shiftEnterForANew") : t("sendShortcut.enterForANewLine")}</p>
    </Group>
  );
}
