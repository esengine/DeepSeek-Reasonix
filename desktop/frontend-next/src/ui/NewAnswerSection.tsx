import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onNewAnswerMarkerChange, setShowsNewAnswerMarker, showsNewAnswerMarker } from "../state/newAnswers";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function NewAnswerSection() {
  const on = useSyncExternalStore(onNewAnswerMarkerChange, showsNewAnswerMarker, showsNewAnswerMarker);
  return (
    <Group id="new-answer-marker" inline title={t("settings.new_answer_divider")} hint={t("settings.divider_before_new_answer")}>
      <Switch data-action="appearance.new-answer-marker" on={on} label={t("settings.new_answer_divider")} onClick={() => setShowsNewAnswerMarker(!on)} />
    </Group>
  );
}
