import { useMemo, type CSSProperties } from "react";
import { themePackKind, type ThemePackView } from "../lib/themePack";
import { baseStyleForPreview, themePreviewPalette } from "../lib/themePreviewPalette";

export function themePreviewPaneAlpha(pack: ThemePackView | null, scene: "home" | "task"): number {
  // Live pane transparency is guarded by data-theme-has-bg. Keep token-only
  // and base previews opaque so the isolated preview matches the applied UI.
  if (pack?.hasBackground !== true) return 1;
  const paneAlpha = scene === "home"
    ? pack.background?.paneOpacity ?? 0.50
    : pack.taskBackground?.paneOpacity ?? pack.background?.paneOpacity ?? 0.68;
  return Math.min(1, Math.max(0, paneAlpha));
}

/** Isolated mini Reasonix surface for gallery detail — does not touch :root. */
export function ThemePreviewSurface({
  pack,
  mode,
  scene,
  variant = "full",
}: {
  pack: ThemePackView | null;
  mode: "light" | "dark";
  scene: "home" | "task";
  variant?: "full" | "thumbnail";
}) {
  const isBasePreview = Boolean(pack && themePackKind(pack) === "base");
  const baseStyle = baseStyleForPreview(pack);
  const style = useMemo(() => {
    const palette = themePreviewPalette(pack, mode);
    const tokens = mode === "light" ? pack?.tokens?.light : pack?.tokens?.dark;
    const chat = tokens?.chat || palette.bg;
    const sceneBackground = scene === "task" ? pack?.taskBackground || pack?.background : pack?.background;
    const focusX = sceneBackground?.focusX ?? 0.72;
    const focusY = sceneBackground?.focusY ?? 0.45;
    const opacity =
      scene === "home"
        ? pack?.background?.homeOpacity ?? 1
        : pack?.taskBackground?.opacity ?? pack?.background?.taskOpacity ?? 0.28;
    const overlay = scene === "task"
      ? pack?.taskBackground?.overlayStrength ?? pack?.background?.overlayStrength ?? 0.62
      : pack?.background?.overlayStrength ?? 0.62;
    const boundedPaneAlpha = themePreviewPaneAlpha(pack, scene);
    const paneCardOffset = scene === "home" ? 0.26 : 0.14;
    return {
      ["--tp-bg" as string]: palette.bg,
      ["--tp-panel" as string]: palette.panel,
      ["--tp-sidebar" as string]: palette.sidebar,
      ["--tp-fg" as string]: palette.fg,
      ["--tp-fg-dim" as string]: palette.fgDim,
      ["--tp-accent" as string]: palette.accent,
      ["--tp-accent-fg" as string]: palette.accentFg,
      ["--tp-border" as string]: palette.border,
      ["--tp-radius" as string]: palette.radius,
      ["--tp-chat" as string]: chat,
      ["--tp-focus-x" as string]: `${focusX * 100}%`,
      ["--tp-focus-y" as string]: `${focusY * 100}%`,
      ["--tp-bg-opacity" as string]: String(opacity),
      ["--tp-overlay" as string]: String(overlay),
      ["--tp-pane-shell-pct" as string]: `${Math.min((boundedPaneAlpha + 0.08) * 100, 100)}%`,
      ["--tp-pane-card-pct" as string]: `${Math.min((boundedPaneAlpha + paneCardOffset) * 100, 100)}%`,
    } as CSSProperties;
  }, [pack, mode, scene]);

  const bgUrl = isBasePreview
    ? ""
    : scene === "task"
      ? pack?.taskBackgroundUrl || pack?.previewUrl || pack?.backgroundUrl || ""
      : pack?.previewUrl || pack?.backgroundUrl || "";

  return (
    <div
      className="theme-preview-surface"
      data-mode={mode}
      data-scene={scene}
      data-preview-kind={isBasePreview ? "base" : "pack"}
      data-base-style={baseStyle}
      data-variant={variant}
      style={style}
    >
      {bgUrl ? (
        <div
          className="theme-preview-surface__bg"
          style={{ backgroundImage: `url("${bgUrl}")` }}
          aria-hidden="true"
        />
      ) : (
        <div
          className={`theme-preview-surface__bg ${isBasePreview ? "theme-preview-surface__bg--base" : "theme-preview-surface__bg--swatch"}`}
          aria-hidden="true"
        />
      )}
      <div className="theme-preview-surface__overlay" aria-hidden="true" />
      <div className="theme-preview-surface__chrome">
        <aside className="theme-preview-surface__side">
          <div className="theme-preview-surface__logo">R</div>
          <div className="theme-preview-surface__nav" />
          <div className="theme-preview-surface__nav theme-preview-surface__nav--dim" />
          <div className="theme-preview-surface__nav theme-preview-surface__nav--dim" />
        </aside>
        <main className="theme-preview-surface__main">
          <div className="theme-preview-surface__card">
            <div className="theme-preview-surface__title" />
            <div className="theme-preview-surface__line" />
            <div className="theme-preview-surface__line theme-preview-surface__line--short" />
            <div className="theme-preview-surface__cta" />
          </div>
        </main>
      </div>
    </div>
  );
}
