import { isThemeStyle, type ThemeStyle } from "./theme";
import { themePackKind, type ThemePackView } from "./themePack";

export type ThemePreviewMode = "light" | "dark";

export type ThemePreviewPalette = {
  bg: string;
  panel: string;
  sidebar: string;
  fg: string;
  fgDim: string;
  accent: string;
  accentFg: string;
  border: string;
  radius: string;
};

/**
 * Canonical miniature palettes for the six native appearance directions.
 * These mirror the semantic variables in styles.css so every gallery surface
 * previews the selected base style instead of falling back to Graphite.
 */
export const BASE_STYLE_PREVIEW_PALETTES: Record<ThemeStyle, Record<ThemePreviewMode, ThemePreviewPalette>> = {
  graphite: {
    dark: { bg: "#0c0d10", panel: "#15161a", sidebar: "#15161a", fg: "#f1f1ef", fgDim: "#a7a8ad", accent: "#ff6a3d", accentFg: "#0c0d10", border: "rgba(255, 255, 255, 0.1)", radius: "8px" },
    light: { bg: "#f4f3ef", panel: "#ffffff", sidebar: "#ffffff", fg: "#16181d", fgDim: "#4a4d56", accent: "#ff5a2c", accentFg: "#ffffff", border: "rgba(20, 22, 28, 0.12)", radius: "8px" },
  },
  aurora: {
    dark: { bg: "#0e0d18", panel: "#17162a", sidebar: "#17162a", fg: "#ecebf7", fgDim: "#a9a4c6", accent: "#8b7cff", accentFg: "#0e0d18", border: "rgba(255, 255, 255, 0.07)", radius: "15px" },
    light: { bg: "#f6f3fb", panel: "#fdfcff", sidebar: "#fdfcff", fg: "#1c1830", fgDim: "#574f70", accent: "#6d5efc", accentFg: "#ffffff", border: "rgba(40, 22, 84, 0.09)", radius: "15px" },
  },
  slate: {
    dark: { bg: "#0d0f12", panel: "#15181d", sidebar: "#15181d", fg: "#e7eaf0", fgDim: "#9aa2b1", accent: "#4d8df6", accentFg: "#061129", border: "rgba(255, 255, 255, 0.08)", radius: "12px" },
    light: { bg: "#f5f6f9", panel: "#ffffff", sidebar: "#ffffff", fg: "#15181f", fgDim: "#4a5160", accent: "#2f6fe0", accentFg: "#ffffff", border: "rgba(20, 28, 46, 0.1)", radius: "12px" },
  },
  carbon: {
    dark: { bg: "#0e0d0c", panel: "#171614", sidebar: "#171614", fg: "#ede9e3", fgDim: "#a59f95", accent: "#2dd4bf", accentFg: "#001a16", border: "rgba(255, 250, 240, 0.08)", radius: "10px" },
    light: { bg: "#f6f4f0", panel: "#ffffff", sidebar: "#ffffff", fg: "#1a1714", fgDim: "#544e46", accent: "#0d9488", accentFg: "#ffffff", border: "rgba(30, 26, 20, 0.1)", radius: "10px" },
  },
  nocturne: {
    dark: { bg: "#101019", panel: "#191a27", sidebar: "#191a27", fg: "#eceaf3", fgDim: "#a6a2bd", accent: "#818cf8", accentFg: "#0d1024", border: "rgba(255, 255, 255, 0.08)", radius: "16px" },
    light: { bg: "#f6f5fb", panel: "#fdfcff", sidebar: "#fdfcff", fg: "#1b1830", fgDim: "#544f6e", accent: "#6366f1", accentFg: "#ffffff", border: "rgba(40, 30, 80, 0.09)", radius: "16px" },
  },
  amber: {
    dark: { bg: "#090a0c", panel: "#191b22", sidebar: "#0c0e12", fg: "#f4f5f7", fgDim: "#c0c4cc", accent: "#d4632f", accentFg: "#1a0a04", border: "#343945", radius: "8px" },
    light: { bg: "#f7f8fb", panel: "#ffffff", sidebar: "#f3f6fa", fg: "#111827", fgDim: "#4b5563", accent: "#dd5b28", accentFg: "#ffffff", border: "#d8dee8", radius: "8px" },
  },
};

export function baseStyleForPreview(pack: ThemePackView | null): ThemeStyle {
  if (isThemeStyle(pack?.baseStyle)) return pack.baseStyle;
  if (isThemeStyle(pack?.id)) return pack.id;
  return "graphite";
}

export function themePreviewPalette(pack: ThemePackView | null, mode: ThemePreviewMode): ThemePreviewPalette {
  const base = baseStyleForPreview(pack);
  const basePalette = BASE_STYLE_PREVIEW_PALETTES[base][mode];
  if (!pack || themePackKind(pack) === "base") return basePalette;

  const tokens = mode === "light" ? pack.tokens?.light : pack.tokens?.dark;
  return {
    bg: tokens?.bg || basePalette.bg,
    panel: tokens?.panel || tokens?.bgElev || basePalette.panel,
    sidebar: tokens?.sidebar || basePalette.sidebar,
    fg: tokens?.fg || basePalette.fg,
    fgDim: tokens?.fgDim || basePalette.fgDim,
    accent: tokens?.accent || basePalette.accent,
    accentFg: tokens?.accentFg || basePalette.accentFg,
    border: tokens?.border || basePalette.border,
    radius: basePalette.radius,
  };
}
