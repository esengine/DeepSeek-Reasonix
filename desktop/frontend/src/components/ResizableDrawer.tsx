import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useT } from "../lib/i18n";
import { loadLayoutSize, saveLayoutSize, type LayoutSizeKey } from "../lib/layoutPreferences";
import { createRafResizeUpdater } from "../lib/resizeDrag";
import { useDeferredClose } from "../lib/useMountTransition";

const DRAWER_DEFAULT_WIDTH = 440;
const DRAWER_MIN_WIDTH = 360;
const DRAWER_MAX_WIDTH = 760;
const DRAWER_MAX_RATIO = 0.62;
const SETTINGS_DRAWER_DEFAULT_WIDTH = 720;
const SETTINGS_DRAWER_MIN_WIDTH = 620;
const SETTINGS_DRAWER_MAX_WIDTH = 1120;
const SETTINGS_DRAWER_MAX_RATIO = 0.82;

function drawerConfig(wide: boolean) {
  return wide
    ? {
        key: "settingsDrawerWidth" as LayoutSizeKey,
        defaultWidth: SETTINGS_DRAWER_DEFAULT_WIDTH,
        minWidth: SETTINGS_DRAWER_MIN_WIDTH,
        maxWidth: SETTINGS_DRAWER_MAX_WIDTH,
        maxRatio: SETTINGS_DRAWER_MAX_RATIO,
      }
    : {
        key: "drawerWidth" as LayoutSizeKey,
        defaultWidth: DRAWER_DEFAULT_WIDTH,
        minWidth: DRAWER_MIN_WIDTH,
        maxWidth: DRAWER_MAX_WIDTH,
        maxRatio: DRAWER_MAX_RATIO,
      };
}

function clampDrawerWidth(width: number, wide: boolean, viewportWidth = 1440): number {
  const config = drawerConfig(wide);
  const maxByViewport = Math.floor(viewportWidth * config.maxRatio);
  const max = Math.max(config.minWidth, Math.min(config.maxWidth, maxByViewport));
  return Math.min(max, Math.max(config.minWidth, Math.round(width)));
}

export function ResizableDrawer({
  children,
  onClose,
  subtle = false,
  wide = false,
}: {
  children: ReactNode;
  onClose: () => void;
  subtle?: boolean;
  wide?: boolean;
}) {
  const t = useT();
  const config = drawerConfig(wide);
  const drawerRef = useRef<HTMLElement>(null);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [width, setWidth] = useState(() =>
    loadLayoutSize(config.key, config.defaultWidth, (value) => clampDrawerWidth(value, wide)),
  );
  const [resizing, setResizing] = useState(false);
  // Slide the drawer back out before the parent unmounts it.
  const { status, requestClose } = useDeferredClose(onClose, 240);
  const effectiveWidth = useMemo(() => clampDrawerWidth(width, wide, viewportWidth), [viewportWidth, wide, width]);
  const style = useMemo(() => ({ "--drawer-width": `${effectiveWidth}px` }) as CSSProperties, [effectiveWidth]);

  useEffect(() => {
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const saveWidth = useCallback(
    (nextWidth: number) => {
      const next = clampDrawerWidth(nextWidth, wide, viewportWidth);
      setWidth(next);
      saveLayoutSize(config.key, next);
    },
    [config.key, viewportWidth, wide],
  );

  const startResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) return;
      const drawer = drawerRef.current;
      if (!drawer) return;
      event.preventDefault();
      setResizing(true);
      let nextWidth = effectiveWidth;
      const liveResize = createRafResizeUpdater({
        target: drawer,
        separator: event.currentTarget,
        cssVar: "--drawer-width",
      });
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampDrawerWidth(window.innerWidth - moveEvent.clientX, wide, window.innerWidth);
        liveResize.schedule(nextWidth);
      };
      const onDone = () => {
        liveResize.flush();
        setWidth(nextWidth);
        saveLayoutSize(config.key, nextWidth);
        setResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [config.key, effectiveWidth, wide],
  );

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        saveWidth(effectiveWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        saveWidth(config.minWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        saveWidth(config.maxWidth);
      }
    },
    [config.maxWidth, config.minWidth, effectiveWidth, saveWidth],
  );

  return (
    <div className={`drawer-backdrop${subtle ? " drawer-backdrop--subtle" : ""}`} data-state={status} onClick={requestClose}>
      <aside
        ref={drawerRef}
        className={`drawer${wide ? " drawer--wide" : ""}${resizing ? " drawer--resizing" : ""}`}
        data-state={status}
        onClick={(e) => e.stopPropagation()}
        style={style}
      >
        <button
          className="drawer-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("drawer.resize")}
          aria-valuemin={config.minWidth}
          aria-valuemax={config.maxWidth}
          aria-valuenow={effectiveWidth}
          onPointerDown={startResize}
          onKeyDown={onKeyDown}
          onDoubleClick={() => saveWidth(config.defaultWidth)}
        />
        {children}
      </aside>
    </div>
  );
}
