import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { hidesLegacySessions, onHidesLegacySessionsChange, setHidesLegacySessions } from "../state/prefs";
import { Group } from "./Group";

export function LegacySessionsSection() {
  const hidden = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  return (
    <Group id="legacy-sessions" title={t("data.sessions_from_1_x")} hint={t("data.whether_sessions_migrated_from_reasonix")}>
      <div className="seg" data-text role="group" aria-label={t("data.sessions_from_1_x")}>
        <button data-action="appearance.legacy-sessions" data-value="show" aria-pressed={!hidden} onClick={() => setHidesLegacySessions(false)}>{t("common.show")}</button>
        <button data-action="appearance.legacy-sessions" data-value="hide" aria-pressed={hidden} onClick={() => setHidesLegacySessions(true)}>{t("data.hide")}</button>
      </div>
    </Group>
  );
}
