import { useEffect, useRef, useSyncExternalStore, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { effectsMode, onEffectsChange } from "../state/prefs";
import type { Wallpaper } from "../port/look";
import { onPresence, present } from "./presence";

// Sized like the picture's pseudo-element in app.css, and kept here rather than
// in the stylesheet: a rule only this element wears belongs beside it.
const LAYER: CSSProperties = {
  position: "fixed", inset: 0, zIndex: -2, pointerEvents: "none",
  width: "calc(100vw / var(--zoom, 1))", height: "calc(100vh / var(--zoom, 1))",
  objectFit: "cover", objectPosition: "var(--bg-x, 50%) var(--bg-y, 50%)",
  opacity: "var(--bg-alpha, 0)", transition: "opacity var(--t-panel) var(--ease)",
};

/** The wallpaper when it is a video: a muted, looping element behind the
 *  window, sized and placed from the same variables a picture uses. It plays
 *  only while someone is looking, and never under reduced motion or reduced
 *  effects, where it stays on its first frame: a moving background costs the
 *  GPU a frame each time and is the first thing to take away from a reader. */
export function WallpaperVideo({ wallpaper }: { wallpaper?: Wallpaper }) {
  const el = useRef<HTMLVideoElement>(null);
  const effects = useSyncExternalStore(onEffectsChange, effectsMode, effectsMode);
  const url = wallpaper?.kind === "video" ? wallpaper.url : "";

  useEffect(() => {
    const video = el.current;
    if (!video || !url) return;
    const still = matchMedia("(prefers-reduced-motion: reduce)");
    const sync = () => {
      if (still.matches || effects === "reduced" || !present()) video.pause();
      else void video.play().catch(() => {});
    };
    sync();
    still.addEventListener("change", sync);
    const unwatch = onPresence(sync);
    return () => {
      still.removeEventListener("change", sync);
      unwatch();
    };
  }, [url, effects]);

  if (!url) return null;
  // On the body, beside the pseudo-element a picture is drawn on: inside the
  // app root, its own stacking context would decide what "behind" means.
  return createPortal(
    <video
      ref={el}
      className="wallpaper-video"
      style={LAYER}
      src={url}
      muted
      loop
      playsInline
      preload="auto"
      aria-hidden="true"
      tabIndex={-1}
      disablePictureInPicture
    />,
    document.body,
  );
}
