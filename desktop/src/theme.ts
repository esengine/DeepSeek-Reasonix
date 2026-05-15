export const THEME = {
  DARK: "dark",
  LIGHT: "light",
} as const;

export type Theme = (typeof THEME)[keyof typeof THEME];

export function isTheme(value: unknown): value is Theme {
  return value === THEME.DARK || value === THEME.LIGHT;
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
} as const;

export type FontFamily = (typeof FONT_FAMILY)[keyof typeof FONT_FAMILY];

export function isFontFamily(value: unknown): value is FontFamily {
  return value === FONT_FAMILY.SANS || value === FONT_FAMILY.SYSTEM || value === FONT_FAMILY.SERIF;
}

export const FONT_FAMILY_STACK: Record<FontFamily, string> = {
  sans: '"IBM Plex Sans", -apple-system, system-ui, sans-serif',
  system: '-apple-system, system-ui, "Segoe UI", Roboto, sans-serif',
  serif: '"IBM Plex Serif", "IBM Plex Sans", Georgia, serif',
};