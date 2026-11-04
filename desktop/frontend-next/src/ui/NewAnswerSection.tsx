import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onNewAnswerMarkerChange, setShowsNewAnswerMarker, showsNewAnswerMarker } from "../state/newAnswers";
import { Group } from "./Group";

export function NewAnswerSection() {
  const on = useSyncExternalStore(onNewAnswerMarkerChange, showsNewAnswerMarker, showsNewAnswerMarker);
  return (
    <Group id="new-answer-marker" title={t("settings.new_answer_divider")} hint={t("settings.when_a_session_with_unread")}>
      <div className="seg" data-text role="group" aria-label={t("settings.new_answer_divider")}>
        <button data-action="appearance.new-answer-marker" data-value="off" aria-pressed={!on} onClick={() => setShowsNewAnswerMarker(false)}>{t("common.close")}</button>
        <button data-action="appearance.new-answer-marker" data-value="on" aria-pressed={on} onClick={() => setShowsNewAnswerMarker(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
