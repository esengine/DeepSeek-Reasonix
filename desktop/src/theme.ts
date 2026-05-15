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