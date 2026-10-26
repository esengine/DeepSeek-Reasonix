import { createPortal } from "react-dom";
import type { Appearance } from "../port/port";
import type { Wallpaper } from "../port/look";
import { t } from "../i18n";

interface Props {
  look: Appearance;
  onLook: (look: Appearance) => void;
}

/** A scene's soundtrack control: the icon is the mute switch, and the vertical
 *  level appears while the pointer or keyboard focus is on it. */
export function WallpaperVolume({ look, onLook }: Props) {
  const paper = look.wallpaper;
  if (!paper || paper.kind !== "scene" || !hasAudio(paper)) return null;
  const volume = clamp(paper.volume ?? 1);
  const silent = paper.muted || volume <= 0;
  const update = (patch: Partial<Wallpaper>) => onLook({ ...look, wallpaper: { ...paper, ...patch } });

  return createPortal(
    <div
      className="wvol"
      data-silent={silent ? "" : undefined}
      style={{ position: "fixed", right: 8, bottom: 8, zIndex: 25, display: "flex", flexDirection: "column", alignItems: "center", gap: 6 }}
    >
      <div className="wvol-pop">
        <input
          className="wvol-range"
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={volume}
          aria-label={t("音量")}
          data-action="wallpaper.volume"
          style={{ writingMode: "vertical-lr", direction: "rtl", width: 18, height: 96, margin: "4px 0", accentColor: "var(--accent)", cursor: "pointer" }}
          onChange={(e) => {
            const next = Number(e.target.value);
            update({ volume: next, muted: next <= 0 });
          }}
        />
      </div>
      <button
        type="button"
        className="wvol-btn"
        data-action="wallpaper.sound"
        aria-label={silent ? t("打开声音") : t("关闭声音")}
        title={silent ? t("打开声音") : t("关闭声音")}
        style={{ width: 34, height: 34, display: "grid", placeItems: "center", padding: 0, border: "1px solid var(--hair)", borderRadius: "50%", background: "color-mix(in srgb, var(--raised) 84%, transparent)", color: "var(--text)", cursor: "pointer", backdropFilter: "blur(12px)" }}
        onClick={() => {
          if (silent) update({ muted: false, volume: volume > 0 ? volume : 1 });
          else update({ muted: true });
        }}
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M4 9h4l5-4v14l-5-4H4z" />
          {silent ? (
            <path d="M17 9l5 6M22 9l-5 6" />
          ) : (
            <>
              <path d="M16 8a5 5 0 0 1 0 8" />
              <path d="M18.5 5.5a9 9 0 0 1 0 13" />
            </>
          )}
        </svg>
      </button>
    </div>,
    document.body,
  );
}

function hasAudio(paper: Wallpaper): boolean {
  return Boolean(paper.audioTracks?.length || paper.audios?.length || paper.audio);
}

function clamp(value: number): number {
  return Math.min(1, Math.max(0, value));
}
