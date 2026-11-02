import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onComposerHintChange, setShowsComposerHint, showsComposerHint } from "../state/prefs";
import { Group } from "./Group";

export function ComposerHintSection() {
  const on = useSyncExternalStore(onComposerHintChange, showsComposerHint, showsComposerHint);
  return (
    <Group id="composer-hint" title={t("输入框提示")} hint={t("在空白任务输入框中显示任务和排队提示。默认显示，仅保存在本机，不影响输入和发送。")}>
      <div className="seg" data-text role="group" aria-label={t("输入框提示")}>
        <button data-action="appearance.composer-hint" data-value="off" aria-pressed={!on} onClick={() => setShowsComposerHint(false)}>{t("关闭")}</button>
        <button data-action="appearance.composer-hint" data-value="on" aria-pressed={on} onClick={() => setShowsComposerHint(true)}>{t("显示")}</button>
      </div>
    </Group>
  );
}
