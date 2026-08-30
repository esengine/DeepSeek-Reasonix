import { useId } from "react";

import { useT } from "../lib/i18n";
import {
  NOTIFICATION_VOLUME_MAX,
  NOTIFICATION_VOLUME_MIN,
  normalizeNotificationVolume,
} from "../lib/sound";

export function NotificationVolumeSlider({
  value,
  onChange,
}: {
  value: number;
  onChange: (value: number) => void;
}) {
  const t = useT();
  const inputId = useId();
  const normalized = normalizeNotificationVolume(value);

  return (
    <div className="notification-volume-control">
      <input
        id={inputId}
        type="range"
        min={NOTIFICATION_VOLUME_MIN}
        max={NOTIFICATION_VOLUME_MAX}
        step={5}
        value={normalized}
        aria-label={t("settings.notificationVolume")}
        aria-valuetext={`${normalized}%`}
        onChange={(event) => onChange(normalizeNotificationVolume(event.currentTarget.value))}
      />
      <output htmlFor={inputId}>{normalized}%</output>
    </div>
  );
}
