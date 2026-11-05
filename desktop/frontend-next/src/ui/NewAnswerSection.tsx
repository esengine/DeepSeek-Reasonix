import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onNewAnswerMarkerChange, setShowsNewAnswerMarker, showsNewAnswerMarker } from "../state/newAnswers";
import { Group } from "./Group";

export function NewAnswerSection() {
  const on = useSyncExternalStore(onNewAnswerMarkerChange, showsNewAnswerMarker, showsNewAnswerMarker);
  return (
    <Group id="new-answer-marker" inline title={t("settings.new_answer_divider")} hint={t("settings.divider_before_new_answer")}>
      <div className="seg" data-text role="group" aria-label={t("settings.new_answer_divider")}>
        <button data-action="appearance.new-answer-marker" data-value="off" aria-pressed={!on} onClick={() => setShowsNewAnswerMarker(false)}>{t("common.close")}</button>
        <button data-action="appearance.new-answer-marker" data-value="on" aria-pressed={on} onClick={() => setShowsNewAnswerMarker(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
