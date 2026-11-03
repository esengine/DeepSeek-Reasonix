import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { LIVE_GRACE_CHOICES, liveGraceMinutes, onLiveGraceChange, setLiveGraceMinutes } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<number, string> = { 0: "liveGraceSection.off", 10: "liveGraceSection.10Minutes", 30: "liveGraceSection.30Minutes", 60: "liveGraceSection.1Hour", 240: "liveGraceSection.4Hours" };

export function LiveGraceSection() {
  const minutes = useSyncExternalStore(onLiveGraceChange, liveGraceMinutes, liveGraceMinutes);
  return (
    <Group id="live-grace" title={t("common.keepFinishedSessionsInRunning")} hint={t("liveGraceSection.afterASessionFinishesA")}>
      <div className="seg" data-text role="group" aria-label={t("common.keepFinishedSessionsInRunning")}>
        {LIVE_GRACE_CHOICES.map((m) => (
          <button key={m} data-action="appearance.live-grace" data-value={String(m)} aria-pressed={minutes === m} onClick={() => setLiveGraceMinutes(m)}>
            {t(LABEL[m])}
          </button>
        ))}
      </div>
    </Group>
  );
}
