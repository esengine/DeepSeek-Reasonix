import { t } from "../i18n";
import { setSendShortcut, useSendShortcut } from "../state/prefs";
import { Group } from "./Group";
import { chord } from "./keys";

export function SendShortcut() {
  const mode = useSendShortcut();
  return (
    <Group id="send-shortcut" title={t("发送快捷键")} hint={t("用于任务输入、待发送消息编辑与历史消息重发。输入法确认候选词时不会发送。")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("发送快捷键")}>
        <button role="radio" data-action="composer.send-shortcut" data-value="enter" aria-checked={mode === "enter"} onClick={() => setSendShortcut("enter")}>{t("Enter 发送")}</button>
        <button role="radio" data-action="composer.send-shortcut" data-value="modifier_enter" aria-checked={mode === "modifier_enter"} onClick={() => setSendShortcut("modifier_enter")}>{t("{key} 发送", { key: chord("Enter") })}</button>
      </div>
      <p className="hint">{mode === "enter" ? t("Shift+Enter 换行；触屏设备使用发送按钮。") : t("Enter 换行，也可点击发送按钮。")}</p>
    </Group>
  );
}
