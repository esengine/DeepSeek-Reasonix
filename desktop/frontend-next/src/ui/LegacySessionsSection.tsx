import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { hidesLegacySessions, onHidesLegacySessionsChange, setHidesLegacySessions } from "../state/prefs";
import { Group } from "./Group";

export function LegacySessionsSection() {
  const hidden = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  return (
    <Group id="legacy-sessions" title={t("common.sessionsFrom1X")} hint={t("legacySessionsSection.whetherSessionsMigratedFromReasonix")}>
      <div className="seg" data-text role="group" aria-label={t("common.sessionsFrom1X")}>
        <button data-action="appearance.legacy-sessions" data-value="show" aria-pressed={!hidden} onClick={() => setHidesLegacySessions(false)}>{t("common.show")}</button>
        <button data-action="appearance.legacy-sessions" data-value="hide" aria-pressed={hidden} onClick={() => setHidesLegacySessions(true)}>{t("common.hide")}</button>
      </div>
    </Group>
  );
}
