import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { LIVE_GRACE_CHOICES, liveGraceMinutes, onLiveGraceChange, setLiveGraceMinutes } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<number, string> = { 0: "settings.off", 10: "settings.n_10_minutes", 30: "settings.n_30_minutes", 60: "settings.n_1_hour", 240: "settings.n_4_hours" };

export function LiveGraceSection() {
  const minutes = useSyncExternalStore(onLiveGraceChange, liveGraceMinutes, liveGraceMinutes);
  return (
    <Group id="live-grace" inline title={t("settings.keep_finished_sessions_in_running")} hint={t("settings.finished_sessions_stay_in_running")}>
      <div className="seg" data-text role="group" aria-label={t("settings.keep_finished_sessions_in_running")}>
        {LIVE_GRACE_CHOICES.map((m) => (
          <button key={m} data-action="appearance.live-grace" data-value={String(m)} aria-pressed={minutes === m} onClick={() => setLiveGraceMinutes(m)}>
            {t(LABEL[m])}
          </button>
        ))}
      </div>
    </Group>
  );
}
