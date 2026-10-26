import { createPortal } from "react-dom";
import { useEffect, useRef } from "react";
import type { Wallpaper, WallpaperAudioTrack } from "../port/look";
import { listenAction } from "./listen";

/** The part of a Wallpaper Engine scene this window can replay: the extracted
 *  backdrop moves a little with the pointer, and its soundtracks loop behind
 *  the interface. Shaders and particles stay in Wallpaper Engine. */
export function WallpaperScene({ wallpaper }: { wallpaper?: Wallpaper }) {
  const audio = useRef<(HTMLAudioElement | null)[]>([]);
  const tracks = audioTracks(wallpaper);
  const trackKey = tracks.map((track) => `${track.url}:${track.volume}:${track.loop}`).join("|");

  useEffect(() => {
    const paper = wallpaper;
    if (!paper || paper.kind !== "scene" || !paper.parallax) return;
    const root = document.documentElement;
    const baseX = clampPct(paper.focusX * 100);
    const baseY = clampPct(paper.focusY * 100);
    const shift = Math.min(0.15, Math.max(0, paper.parallax)) * 100;
    const reduced = () =>
      root.dataset.effects === "reduced" ||
      (typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches);
    const move = (event: PointerEvent) => {
      if (reduced()) return;
      const dx = (event.clientX / Math.max(innerWidth, 1) - 0.5) * shift;
      const dy = (event.clientY / Math.max(innerHeight, 1) - 0.5) * shift;
      root.style.setProperty("--bg-x", `${clampPct(baseX - dx)}%`);
      root.style.setProperty("--bg-y", `${clampPct(baseY - dy)}%`);
    };
    const off = listenAction(window, "pointermove", { action: "wallpaper.parallax", listener: move as EventListener }, { passive: true });
    return () => {
      off();
      root.style.setProperty("--bg-x", `${baseX}%`);
      root.style.setProperty("--bg-y", `${baseY}%`);
    };
  }, [wallpaper?.kind, wallpaper?.parallax, wallpaper?.focusX, wallpaper?.focusY]);

  useEffect(() => {
    tracks.forEach((track, i) => {
      const element = audio.current[i];
      if (element) element.volume = clampLevel((wallpaper?.volume ?? 1) * track.volume);
    });
  }, [wallpaper?.volume, trackKey]);

  useEffect(() => {
    if (!tracks.length || wallpaper?.kind !== "scene" || wallpaper.muted || (wallpaper.volume ?? 1) <= 0) return;
    const start = () => {
      for (const element of audio.current) {
        if (!element) continue;
        try {
          void Promise.resolve(element.play()).catch(() => {});
        } catch {
          // Autoplay is refused until a gesture; the listeners below retry.
        }
      }
    };
    start();
    const offPointer = listenAction(window, "pointerdown", { action: "wallpaper.sound", listener: start as EventListener }, { once: true });
    const offKey = listenAction(window, "keydown", { action: "wallpaper.sound", listener: start as EventListener }, { once: true });
    return () => {
      offPointer();
      offKey();
      for (const element of audio.current) element?.pause();
    };
  }, [trackKey, wallpaper?.muted, wallpaper?.kind, wallpaper?.volume]);

  if (!tracks.length) return null;
  return createPortal(
    <>
      {tracks.map((track, i) => (
        <audio
          key={`${i}:${track.url}`}
          ref={(element) => {
            audio.current[i] = element;
          }}
          src={track.url}
          loop={track.loop}
          muted={wallpaper?.muted}
          preload="auto"
          hidden
        />
      ))}
    </>,
    document.body,
  );
}

function audioTracks(wallpaper?: Wallpaper): WallpaperAudioTrack[] {
  if (wallpaper?.kind !== "scene") return [];
  if (wallpaper.audioTracks?.length) return wallpaper.audioTracks;
  if (wallpaper.audios?.length) {
    return wallpaper.audios.map((url) => ({ url, volume: 1, loop: true }));
  }
  return wallpaper.audio ? [{ url: wallpaper.audio, volume: 1, loop: true }] : [];
}

function clampPct(value: number): number {
  return Math.min(100, Math.max(0, value));
}

function clampLevel(value: number): number {
  return Math.min(1, Math.max(0, value));
}
