import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onComposerHintChange, setShowsComposerHint, showsComposerHint } from "../state/prefs";
import { Group } from "./Group";

export function ComposerHintSection() {
  const on = useSyncExternalStore(onComposerHintChange, showsComposerHint, showsComposerHint);
  return (
    <Group id="composer-hint" title={t("common.composerHint")} hint={t("composerHintSection.showTaskAndQueueGuidance")}>
      <div className="seg" data-text role="group" aria-label={t("common.composerHint")}>
        <button data-action="appearance.composer-hint" data-value="off" aria-pressed={!on} onClick={() => setShowsComposerHint(false)}>{t("common.close")}</button>
        <button data-action="appearance.composer-hint" data-value="on" aria-pressed={on} onClick={() => setShowsComposerHint(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
