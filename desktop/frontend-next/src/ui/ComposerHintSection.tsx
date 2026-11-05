import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onComposerHintChange, setShowsComposerHint, showsComposerHint } from "../state/prefs";
import { Group } from "./Group";

export function ComposerHintSection() {
  const on = useSyncExternalStore(onComposerHintChange, showsComposerHint, showsComposerHint);
  return (
    <Group id="composer-hint" inline title={t("composer.composer_hint")} hint={t("settings.hint_text_in_the_composer")}>
      <div className="seg" data-text role="group" aria-label={t("composer.composer_hint")}>
        <button data-action="appearance.composer-hint" data-value="off" aria-pressed={!on} onClick={() => setShowsComposerHint(false)}>{t("common.close")}</button>
        <button data-action="appearance.composer-hint" data-value="on" aria-pressed={on} onClick={() => setShowsComposerHint(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
