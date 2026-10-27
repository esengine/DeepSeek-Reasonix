import { createPortal } from "react-dom";
import type { CSSProperties } from "react";
import type { Wallpaper } from "../port/look";

/** A scene's image layers, painted on the same plane as the picture background.
 *  Effects are retained as data for a future shader pass; this component only
 *  composites the extracted images and their transforms. */
export function WallpaperLayers({ wallpaper }: { wallpaper?: Wallpaper }) {
  const paper = wallpaper;
  const layers = paper?.layers;
  if (!paper || !layers?.length) return null;
  const sceneW = paper.sceneWidth && paper.sceneWidth > 0 ? paper.sceneWidth : 1;
  const sceneH = paper.sceneHeight && paper.sceneHeight > 0 ? paper.sceneHeight : 1;
  const aspect = sceneW / sceneH;
  const stage: CSSProperties = {
    position: "absolute",
    left: "50%",
    top: "50%",
    width: `max(calc(100vw / var(--zoom, 1)), calc(100vh / var(--zoom, 1) * ${aspect}))`,
    aspectRatio: `${sceneW} / ${sceneH}`,
    transform: "translate(-50%, -50%)",
  };
  return createPortal(
    <div
      className="wlayers"
      aria-hidden="true"
      style={{
        position: "fixed",
        inset: 0,
        zIndex: -2,
        pointerEvents: "none",
        overflow: "hidden",
        width: "calc(100vw / var(--zoom, 1))",
        height: "calc(100vh / var(--zoom, 1))",
        opacity: "var(--bg-alpha, 0)",
        transition: "opacity var(--t-panel) var(--ease)",
      }}
    >
      <div style={stage}>
        {layers.map((layer, i) => (
          <img
            key={`${i}:${layer.url}`}
            src={layer.url}
            alt=""
            draggable={false}
            data-effects={layer.effects?.length ? layer.effects.join(" ") : undefined}
            style={{
              position: "absolute",
              left: `${layer.x * 100}%`,
              top: `${layer.y * 100}%`,
              width: `${layer.w * 100}%`,
              height: `${layer.h * 100}%`,
              opacity: layer.alpha,
              objectFit: "fill",
            }}
          />
        ))}
      </div>
    </div>,
    document.body,
  );
}
