export const THEME = {
  DARK: "dark",
  LIGHT: "light",
} as const;

export type Theme = (typeof THEME)[keyof typeof THEME];

export function isTheme(value: unknown): value is Theme {
  return value === THEME.DARK || value === THEME.LIGHT;
}

export const THEME_STYLE = {
  GRAPHITE: "graphite",
  SANDSTONE: "sandstone",
  PORCELAIN: "porcelain",
  MIDNIGHT: "midnight",
} as const;

export type ThemeStyle = (typeof THEME_STYLE)[keyof typeof THEME_STYLE];

export const DEFAULT_THEME_STYLE: Record<Theme, ThemeStyle> = {
  dark: THEME_STYLE.GRAPHITE,
  light: THEME_STYLE.SANDSTONE,
};

export const THEME_STYLE_THEME: Record<ThemeStyle, Theme> = {
  graphite: THEME.DARK,
  sandstone: THEME.LIGHT,
  porcelain: THEME.LIGHT,
  midnight: THEME.DARK,
};

export const THEME_STYLES = [
  THEME_STYLE.GRAPHITE,
  THEME_STYLE.SANDSTONE,
  THEME_STYLE.PORCELAIN,
  THEME_STYLE.MIDNIGHT,
] as const;

export function isThemeStyle(value: unknown): value is ThemeStyle {
  return (
    value === THEME_STYLE.GRAPHITE ||
    value === THEME_STYLE.SANDSTONE ||
    value === THEME_STYLE.PORCELAIN ||
    value === THEME_STYLE.MIDNIGHT
  );
}

export function themeForStyle(style: ThemeStyle): Theme {
  return THEME_STYLE_THEME[style];
}

export function defaultStyleForTheme(theme: Theme): ThemeStyle {
  return DEFAULT_THEME_STYLE[theme];
}

export const FONT_SCALE = {
  SMALL: "small",
  MEDIUM: "medium",
  LARGE: "large",
} as const;

export type FontScale = (typeof FONT_SCALE)[keyof typeof FONT_SCALE];

export function isFontScale(value: unknown): value is FontScale {
  return value === FONT_SCALE.SMALL || value === FONT_SCALE.MEDIUM || value === FONT_SCALE.LARGE;
}

export const FONT_SCALE_ZOOM: Record<FontScale, number> = {
  small: 0.875,
  medium: 1.0,
  large: 1.125,
};

export const FONT_FAMILY = {
  SANS: "sans",
  SYSTEM: "system",
  SERIF: "serif",
  CUSTOM: "custom",
} as const;

export type FontFamily = (typeof FONT_FAMILY)[keyof typeof FONT_FAMILY];

export function isFontFamily(value: unknown): value is FontFamily {
  return (
    value === FONT_FAMILY.SANS ||
    value === FONT_FAMILY.SYSTEM ||
    value === FONT_FAMILY.SERIF ||
    value === FONT_FAMILY.CUSTOM
  );
}

export const FONT_FAMILY_STACK: Record<FontFamily, string> = {
  sans: '"Geist", system-ui, sans-serif',
  system: '-apple-system, system-ui, "Segoe UI", Roboto, sans-serif',
  serif: 'Georgia, "Times New Roman", serif',
  custom: '"Geist", system-ui, sans-serif',
};
