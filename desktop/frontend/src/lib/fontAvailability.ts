import { FONT_FAMILIES, MONO_FONT_FAMILIES, type FontFamily, type MonoFontFamily } from "./fontFamily";

export type TypographyPlatform = "windows" | "darwin" | "linux";
type FontProbe = (fontNames: readonly string[]) => boolean;

const UI_FONT_CANDIDATES: Record<Exclude<FontFamily, "system" | "custom">, { platforms: TypographyPlatform[]; fonts: string[] }> = {
  yahei: {
    platforms: ["windows"],
    fonts: ["Microsoft YaHei UI", "Microsoft YaHei", "微软雅黑"],
  },
  pingfang: {
    platforms: ["darwin"],
    fonts: ["PingFang SC", "PingFang TC", "苹方-简"],
  },
  noto: {
    platforms: ["linux"],
    fonts: ["Noto Sans SC", "Noto Sans CJK SC", "Source Han Sans SC", "思源黑体"],
  },
};

const MONO_FONT_CANDIDATES: Record<Exclude<MonoFontFamily, "system" | "custom">, { platforms: TypographyPlatform[]; fonts: string[] }> = {
  cascadia: {
    platforms: ["windows"],
    fonts: ["Cascadia Code", "Cascadia Mono"],
  },
  jetbrains: {
    platforms: [],
    fonts: ["JetBrains Mono"],
  },
  sfmono: {
    platforms: ["darwin"],
    fonts: ["SF Mono", "SFMono-Regular"],
  },
};

export function getTypographyPlatform(): TypographyPlatform {
  if (typeof document !== "undefined") {
    const attr = document.documentElement.getAttribute("data-platform");
    if (attr === "windows" || attr === "darwin" || attr === "linux") return attr;
  }
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

export function getAvailableFontFamilies(
  selected: FontFamily,
  platform: TypographyPlatform = getTypographyPlatform(),
  probe: FontProbe = isAnyLocalFontAvailable,
): FontFamily[] {
  return FONT_FAMILIES.filter((font) => {
    if (font === "system" || font === "custom" || font === selected) return true;
    const meta = UI_FONT_CANDIDATES[font];
    return meta.platforms.includes(platform) || probe(meta.fonts);
  });
}

export function getAvailableMonoFontFamilies(
  selected: MonoFontFamily,
  platform: TypographyPlatform = getTypographyPlatform(),
  probe: FontProbe = isAnyLocalFontAvailable,
): MonoFontFamily[] {
  return MONO_FONT_FAMILIES.filter((font) => {
    if (font === "system" || font === "custom" || font === selected) return true;
    const meta = MONO_FONT_CANDIDATES[font];
    return meta.platforms.includes(platform) || probe(meta.fonts);
  });
}

function isAnyLocalFontAvailable(fontNames: readonly string[]): boolean {
  if (typeof document === "undefined") return false;
  return fontNames.some((font) => isLocalFontAvailable(font));
}

function isLocalFontAvailable(fontName: string): boolean {
  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d");
  if (!ctx) return false;

  const sample = "Reasonix 字体检测 0123456789";
  const size = "72px";
  const fallbacks = ["monospace", "serif", "sans-serif"] as const;
  return fallbacks.some((fallback) => {
    ctx.font = `${size} ${fallback}`;
    const fallbackWidth = ctx.measureText(sample).width;
    ctx.font = `${size} ${quoteFontName(fontName)}, ${fallback}`;
    return Math.abs(ctx.measureText(sample).width - fallbackWidth) > 0.5;
  });
}

function quoteFontName(name: string): string {
  return `"${name.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}
