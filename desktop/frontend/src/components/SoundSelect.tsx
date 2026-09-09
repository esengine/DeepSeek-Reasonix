import { Play } from "lucide-react";
import { SettingsSelect } from "./SettingsSelect";
import { useT } from "../lib/i18n";
import type { DictKey } from "../lib/i18n";
import type { SoundWavPref } from "../lib/sound";

type SoundOption = {
  value: SoundWavPref;
  labelKey: DictKey;
};

const OPTIONS: SoundOption[] = [
  { value: "off", labelKey: "settings.notificationSound.off" },
  { value: "synth", labelKey: "settings.notificationSound.synth" },
  { value: "positive", labelKey: "settings.notificationSound.positive" },
  { value: "correct", labelKey: "settings.notificationSound.correct" },
  { value: "start", labelKey: "settings.notificationSound.start" },
  { value: "back", labelKey: "settings.notificationSound.back" },
];

export function SoundSelect({
  value,
  onChange,
  onPreview,
  previewDisabled,
}: {
  value: SoundWavPref;
  onChange: (v: SoundWavPref) => void;
  onPreview: () => void;
  previewDisabled?: boolean;
}) {
  const t = useT();
  return (
    <div className="sound-select">
      <SettingsSelect value={value} onValueChange={next => onChange(next as SoundWavPref)}
        aria-label={t("settings.notificationSound")}
        options={OPTIONS.map(option => ({ value: option.value, label: t(option.labelKey) }))} />
      {!previewDisabled && (
        <button className="chip chip--icon" type="button" title={t("settings.notificationSoundPreview")} aria-label={t("settings.notificationSoundPreview")} onClick={onPreview}>
          <Play size={13} aria-hidden="true" />
        </button>
      )}

    </div>
  );
}
