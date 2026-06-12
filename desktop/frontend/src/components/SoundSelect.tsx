import { useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";
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
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const selected = OPTIONS.find((o) => o.value === value) ?? OPTIONS[0];

  return (
    <div className="sound-select">
      <button
        ref={triggerRef}
        className="sound-select__trigger"
        type="button"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="sound-select__label">{t(selected.labelKey)}</span>
        <ChevronDown
          size={16}
          className={`sound-select__chev${open ? " sound-select__chev--open" : ""}`}
        />
      </button>
      <button className="chip" type="button" title={t("settings.notificationSoundPreview")} onClick={onPreview} disabled={previewDisabled}>
        &#x25B6;
      </button>
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="sound-select__menu"
        placement="bottom"
      >
        <div className="sound-select__list" role="listbox">
          {OPTIONS.map((opt) => (
            <button
              key={opt.value}
              className={`sound-select__option${opt.value === value ? " sound-select__option--selected" : ""}`}
              role="option"
              aria-selected={opt.value === value}
              type="button"
              onClick={() => {
                onChange(opt.value);
                setOpen(false);
              }}
            >
              <span>{t(opt.labelKey)}</span>
              {opt.value === value && <Check size={14} className="sound-select__check" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}
