import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { LIVE_GRACE_CHOICES, liveGraceMinutes, onLiveGraceChange, setLiveGraceMinutes } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<number, string> = { 0: "不保留", 10: "10 分钟", 30: "30 分钟", 60: "1 小时", 240: "4 小时" };

export function LiveGraceSection() {
  const minutes = useSyncExternalStore(onLiveGraceChange, liveGraceMinutes, liveGraceMinutes);
  return (
    <Group id="live-grace" title={t("settings.keep_finished_sessions_in_running")} hint={t("settings.after_a_session_finishes_a")}>
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
