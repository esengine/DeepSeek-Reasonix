// Measures the transcript surface the launcher card floats over and reports
// whether the card still fits. The card yields the space entirely below
// HIDE_BELOW_WIDTH so it never crowds the chat.
//
// The mode is mirrored onto <html> so siblings OUTSIDE the transcript surface
// (the footer/composer) reserve the same right inset via plain CSS — no
// App-level re-render on every resize tick.
import { useEffect, useState } from "react";
import type { RefObject } from "react";
import type { SpaceMode } from "./launcherCardState";

// Space priority for the docked launcher: the panel yields FIRST. The panel
// only appears when the surface is at least 1010px wide (history keeps ~770px
// of the 800px comfort target while the panel is docked); below that the
// panel hides and the history stretches/compresses to fill the whole window.
const HIDE_BELOW_WIDTH = 1010;

export function useDockLauncherSpace(
  rootRef: RefObject<HTMLElement | null>,
  onSpaceModeChange?: (mode: SpaceMode) => void,
): SpaceMode {
  const [spaceMode, setSpaceMode] = useState<SpaceMode>("full");

  useEffect(() => {
    const host = rootRef.current?.parentElement;
    if (!host || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width ?? 0;
      const next = width < HIDE_BELOW_WIDTH ? "hidden" : "full";
      setSpaceMode(next);
      onSpaceModeChange?.(next);
    });
    observer.observe(host);
    return () => observer.disconnect();
  }, [rootRef, onSpaceModeChange]);

  useEffect(() => {
    document.documentElement.dataset.dockLauncher = spaceMode;
    return () => {
      delete document.documentElement.dataset.dockLauncher;
    };
  }, [spaceMode]);

  return spaceMode;
}
