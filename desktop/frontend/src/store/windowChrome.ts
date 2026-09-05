// windowChrome owns the desktop shell's native chrome state — detected
// desktop platform and viewport geometry — as a selectable store rather than
// App-local useState. Runtime wiring (platform probe, resize listener) lives in
// the app-runtime WindowChromeLifecycle component; components only read slices,
// which keeps every chrome consumer on one source of truth without prop
// drilling and without duplicating listeners per region.

import { create } from "zustand";
import { detectBrowserPlatform } from "../lib/desktopPlatform";
import type { DesktopPlatform } from "../lib/desktopPlatform";

function initialViewportSize(): { width: number; height: number } {
  if (typeof window === "undefined") return { width: 1440, height: 720 };
  return { width: window.innerWidth, height: window.innerHeight };
}

type WindowChromeState = {
  platform: DesktopPlatform;
  viewportWidth: number;
  viewportHeight: number;
};

export const useWindowChromeStore = create<WindowChromeState>(() => {
  const viewport = initialViewportSize();
  return { platform: detectBrowserPlatform(), viewportWidth: viewport.width, viewportHeight: viewport.height };
});

export const setDesktopPlatform = (platform: DesktopPlatform): void => {
  useWindowChromeStore.setState({ platform });
};

export const setViewportSize = (width: number, height: number): void => {
  useWindowChromeStore.setState((current) =>
    current.viewportWidth === width && current.viewportHeight === height ? current : { viewportWidth: width, viewportHeight: height },
  );
};
