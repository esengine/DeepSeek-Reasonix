export const TYPOGRAPHY_REGIONS = ["interface", "conversation", "composer", "code", "metadata"] as const;

export type TypographyRegion = (typeof TYPOGRAPHY_REGIONS)[number];

export const REGION_FONT_FAMILIES = [
  "inherit",
  "system",
  "yahei",
  "pingfang",
  "noto",
  "cascadia",
  "jetbrains",
  "sfmono",
  "custom",
] as const;

export type RegionFontFamily = (typeof REGION_FONT_FAMILIES)[number];

export type RegionTypography = {
  followGlobal: boolean;
  fontFamily: RegionFontFamily;
  customFontName: string;
  fontSize: number;
};

export type TypographyPreferences = Record<TypographyRegion, RegionTypography>;

export const TYPOGRAPHY_STORAGE_KEY = "reasonix-region-typography-v1";

export const TYPOGRAPHY_REGION_META: Record<TypographyRegion, { baseSize: number; min: number; max: number }> = {
  interface: { baseSize: 14, min: 11, max: 20 },
  conversation: { baseSize: 14, min: 12, max: 24 },
  composer: { baseSize: 14, min: 12, max: 24 },
  code: { baseSize: 12, min: 10, max: 22 },
  metadata: { baseSize: 12, min: 9, max: 18 },
};

const FONT_STACKS: Record<RegionFontFamily, string> = {
  inherit: "",
  system: 'var(--font-ui)',
  yahei: '"Microsoft YaHei UI", "Microsoft YaHei", "微软雅黑", "PingFang SC", sans-serif',
  pingfang: '"PingFang SC", "苹方-简", "Noto Sans SC", "Microsoft YaHei", sans-serif',
  noto: '"Noto Sans SC", "Noto Sans", "PingFang SC", "Microsoft YaHei", sans-serif',
  cascadia: '"Cascadia Code", "Cascadia Mono", Consolas, ui-monospace, monospace',
  jetbrains: '"JetBrains Mono", "Cascadia Code", "SF Mono", Consolas, ui-monospace, monospace',
  sfmono: '"SF Mono", SFMono-Regular, ui-monospace, Menlo, Monaco, monospace',
  custom: "",
};

function defaultRegion(region: TypographyRegion): RegionTypography {
  return {
    followGlobal: true,
    fontFamily: "inherit",
    customFontName: "",
    fontSize: TYPOGRAPHY_REGION_META[region].baseSize,
  };
}

export function createDefaultTypographyPreferences(): TypographyPreferences {
  return Object.fromEntries(TYPOGRAPHY_REGIONS.map((region) => [region, defaultRegion(region)])) as TypographyPreferences;
}

export function sanitizeCustomFontName(value: unknown): string {
  if (typeof value !== "string") return "";
  const compact = value.trim().replace(/\s+/g, " ").slice(0, 200);
  return isSafeCustomFontNameInput(compact) ? compact : "";
}

export function isSafeCustomFontNameInput(value: unknown): value is string {
  return typeof value === "string" && !/[;{}<>]/.test(value);
}

export function normalizeTypographyPreferences(value: unknown): TypographyPreferences {
  const defaults = createDefaultTypographyPreferences();
  const source = value && typeof value === "object" ? (value as Record<string, unknown>) : {};

  for (const region of TYPOGRAPHY_REGIONS) {
    const raw = source[region];
    if (!raw || typeof raw !== "object") continue;
    const candidate = raw as Record<string, unknown>;
    const meta = TYPOGRAPHY_REGION_META[region];
    const numericSize = typeof candidate.fontSize === "number" && Number.isFinite(candidate.fontSize)
      ? candidate.fontSize
      : meta.baseSize;
    defaults[region] = {
      followGlobal: candidate.followGlobal !== false,
      fontFamily: typeof candidate.fontFamily === "string" && (REGION_FONT_FAMILIES as readonly string[]).includes(candidate.fontFamily)
        ? candidate.fontFamily as RegionFontFamily
        : "inherit",
      customFontName: sanitizeCustomFontName(candidate.customFontName),
      fontSize: Math.round(Math.min(meta.max, Math.max(meta.min, numericSize))),
    };
  }
  return defaults;
}

export function getTypographyPreferences(): TypographyPreferences {
  if (typeof localStorage === "undefined") return createDefaultTypographyPreferences();
  try {
    const stored = localStorage.getItem(TYPOGRAPHY_STORAGE_KEY);
    return stored ? normalizeTypographyPreferences(JSON.parse(stored)) : createDefaultTypographyPreferences();
  } catch {
    return createDefaultTypographyPreferences();
  }
}

export function fontStackForPreference(preference: RegionTypography): string {
  if (preference.fontFamily === "custom") return sanitizeCustomFontName(preference.customFontName);
  return FONT_STACKS[preference.fontFamily];
}

export function applyTypographyPreferences(preferences: TypographyPreferences): void {
  if (typeof document === "undefined") return;
  const normalized = normalizeTypographyPreferences(preferences);
  const root = document.documentElement;

  for (const region of TYPOGRAPHY_REGIONS) {
    const preference = normalized[region];
    const scaleProperty = `--typography-${region}-scale`;
    const sizeProperty = `--typography-${region}-size`;
    const fontProperty = `--typography-${region}-font`;
    if (preference.followGlobal) {
      root.style.removeProperty(scaleProperty);
      root.style.removeProperty(sizeProperty);
      root.style.removeProperty(fontProperty);
      continue;
    }
    root.style.setProperty(scaleProperty, String(preference.fontSize / TYPOGRAPHY_REGION_META[region].baseSize));
    root.style.setProperty(sizeProperty, `${preference.fontSize}px`);
    const fontStack = fontStackForPreference(preference);
    if (fontStack) root.style.setProperty(fontProperty, fontStack);
    else root.style.removeProperty(fontProperty);
  }

  try {
    localStorage.setItem(TYPOGRAPHY_STORAGE_KEY, JSON.stringify(normalized));
  } catch {
    /* private mode / no storage */
  }
}

export function initTypographyPreferences(): void {
  applyTypographyPreferences(getTypographyPreferences());
}
