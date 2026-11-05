import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { hidesLegacySessions, onHidesLegacySessionsChange, setHidesLegacySessions } from "../state/prefs";
import { SegChoice, SettingRow } from "./SettingRow";

export function LegacySessionsSection() {
  const hidden = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  return (
    <SettingRow id="legacy-sessions" title={t("data.sessions_from_1_x")} desc={t("settings.migrated_sessions_in_the_sidebar")} more={t("data.whether_sessions_migrated_from_reasonix")}>
      <SegChoice label={t("data.sessions_from_1_x")} value={hidden ? "hide" : "show"} action="appearance.legacy-sessions"
        onPick={(v) => setHidesLegacySessions(v === "hide")} options={[["show", t("common.show")], ["hide", t("data.hide")]]} />
    </SettingRow>
  );
}
