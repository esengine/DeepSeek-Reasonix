import type { CSSProperties } from "react";
import type { Wallpaper } from "../port/look";
import { t } from "../i18n";
import { pct } from "../i18n/format";

/** The scene's soundtrack level, drawn with the same slider as the focus
 *  controls beside it. */
export function WallpaperVolumeSlider({ volume, onChange }: { volume?: number; onChange: (patch: Partial<Wallpaper>) => void }) {
  const value = Math.min(1, Math.max(0, volume ?? 1));
  return (
    <label className="prow">
      <span className="tx">{t("音量")}</span>
      <input
        className="slider"
        style={{ "--at": `${Math.round(value * 100)}%` } as CSSProperties}
        type="range"
        min={0}
        max={1}
        step={0.05}
        value={value}
        data-action="wallpaper.volume"
        onChange={(e) => {
          const next = Number(e.target.value);
          onChange({ volume: next, muted: next <= 0 });
        }}
      />
      <span className="now">{pct(value)}</span>
    </label>
  );
}
