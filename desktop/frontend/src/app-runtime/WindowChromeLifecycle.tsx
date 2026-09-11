import { useEffect } from "react";
import { app } from "../lib/bridge";
import { browserPlatformOverride, normalizeDesktopPlatform } from "../lib/desktopPlatform";
import { useDesktopPreferences } from "./useDesktopPreferences";
import { setDesktopPlatform, setViewportSize, useWindowChromeStore } from "../store/windowChrome";
import {
  CREATION_RIGHT_DOCK_TREE_MIN_WIDTH,
  RIGHT_DOCK_TREE_MIN_WIDTH,
  SIDEBAR_MIN_WIDTH,
  saveRightDockTreeWidth,
  saveSidebarWidth,
  useLayoutStore,
} from "../store/layout";

/**
 * Owns the desktop chrome listeners that feed the windowChrome store: the
 * native platform probe, viewport resize, the data-platform attribute and the
 * layout minimum-width guards. Renders nothing; App composes it once beside
 * AppRuntimeEffects so every chrome consumer reads one store.
 */
export function WindowChromeLifecycle() {
  const platform = useWindowChromeStore((state) => state.platform);
  const sidebarWidth = useLayoutStore((state) => state.sidebarWidth);
  const setSidebarWidth = useLayoutStore((state) => state.setSidebarWidth);
  const rightDockTreeWidth = useLayoutStore((state) => state.rightDockTreeWidth);
  const setRightDockTreeWidth = useLayoutStore((state) => state.setRightDockTreeWidth);
  const { desktopLayoutStyle } = useDesktopPreferences();

  useEffect(() => {
    document.documentElement.setAttribute("data-platform", platform);
  }, [platform]);

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => {
      setViewportSize(window.innerWidth, window.innerHeight);
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  useEffect(() => {
    if (desktopLayoutStyle === "creation" || sidebarWidth >= SIDEBAR_MIN_WIDTH) return;
    setSidebarWidth(SIDEBAR_MIN_WIDTH);
    saveSidebarWidth(SIDEBAR_MIN_WIDTH);
  }, [desktopLayoutStyle, setSidebarWidth, sidebarWidth]);

  useEffect(() => {
    if (desktopLayoutStyle === "creation") {
      if (rightDockTreeWidth >= CREATION_RIGHT_DOCK_TREE_MIN_WIDTH) return;
      setRightDockTreeWidth(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH);
      saveRightDockTreeWidth(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH);
      return;
    }
    if (rightDockTreeWidth >= RIGHT_DOCK_TREE_MIN_WIDTH) return;
    setRightDockTreeWidth(RIGHT_DOCK_TREE_MIN_WIDTH);
    saveRightDockTreeWidth(RIGHT_DOCK_TREE_MIN_WIDTH);
  }, [desktopLayoutStyle, rightDockTreeWidth, setRightDockTreeWidth]);

  return null;
}
