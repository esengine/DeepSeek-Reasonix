import { describe, expect, it } from "vitest";

const RAW = import.meta.glob(["./app.css", "./tokens.css", "./TOKENS.md"], {
  query: "?raw", import: "default", eager: true,
}) as Record<string, string>;
const SHEET = RAW["./app.css"];
const TOKENS = RAW["./tokens.css"];
const TOKEN_DOC = RAW["./TOKENS.md"];

type Rgb = [number, number, number];

function linearize(value: number): number {
  const c = value / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

function encode(value: number): number {
  const c = Math.max(0, Math.min(1, value));
  return 255 * (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);
}

function hexToRgb(hex: string): Rgb {
  return [1, 3, 5].map((at) => Number.parseInt(hex.slice(at, at + 2), 16)) as Rgb;
}

function relativeLuminance([r, g, b]: Rgb): number {
  return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b);
}

function contrast(a: Rgb, b: Rgb): number {
  const x = relativeLuminance(a), y = relativeLuminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

function oklchToRgb(lightness: number, chroma: number, hue: number): Rgb {
  const angle = hue * Math.PI / 180;
  const a = chroma * Math.cos(angle), b = chroma * Math.sin(angle);
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.2914855480 * b) ** 3;
  return [
    encode(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    encode(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    encode(-0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s),
  ];
}

function hexToOklch(hex: string): { l: number; c: number } {
  const [r, g, b] = hexToRgb(hex).map(linearize);
  const l = (0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b) ** (1 / 3);
  const m = (0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b) ** (1 / 3);
  const s = (0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b) ** (1 / 3);
  const L = 0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s;
  const a = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s;
  const b2 = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s;
  return { l: L, c: Math.hypot(a, b2) };
}

function minSourceContrast(chart: string, track: Rgb): number {
  const { l, c } = hexToOklch(chart);
  let minimum = Infinity;
  for (let hue = 0; hue < 360; hue++) minimum = Math.min(minimum, contrast(oklchToRgb(l, c, hue), track));
  return minimum;
}

describe("usage remains readable on a phone", () => {
  it("derives source colours from chart tokens that clear 3:1 in both themes", () => {
    const lightChart = SHEET.match(/\.usage\s*\{[^}]*--chart:\s*(#[0-9a-f]{6})/i)?.[1] ?? "";
    const darkChart = SHEET.match(/\[data-theme="dark"\]\s*\.usage\s*\{[^}]*--chart:\s*(#[0-9a-f]{6})/i)?.[1] ?? "";
    expect(lightChart).toBeTruthy();
    expect(darkChart).toBeTruthy();
    expect(TOKENS).toContain("--hair: oklch(91.7% .0126 85)");
    expect(TOKENS).toContain("--hair: oklch(24.2% .016 255)");

    expect(minSourceContrast(lightChart, oklchToRgb(0.917, 0.0126, 85))).toBeGreaterThanOrEqual(3.5);
    expect(minSourceContrast(darkChart, oklchToRgb(0.242, 0.016, 255))).toBeGreaterThanOrEqual(4.5);
  });

  it("lets an explicit source colour override the shared chart hue", () => {
    expect(SHEET).toMatch(/\.ufill\s*\{[^}]*background:\s*var\(--row-color,\s*var\(--chart\)\)/);
  });

  it("uses --muted instead of an undefined --fg-2 token", () => {
    expect(SHEET).not.toContain("--fg-2");
    expect(TOKENS).not.toContain("--fg-2");
    expect(TOKEN_DOC).not.toContain("--fg-2");
    expect(SHEET).toMatch(/\.utable td:not\(:first-child\)\s*\{\s*color:\s*var\(--muted\)/);
  });

  it("keeps the full daily table in a bounded, hinted scroll region", () => {
    const rule = SHEET.match(/\.utable-wrap\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/max-height:\s*\d+px/);
    expect(rule).toMatch(/overflow:\s*auto/);
    const pinned = SHEET.match(/\.utable th:first-child,\s*\.utable td:first-child\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(pinned).toMatch(/position:\s*sticky/);
    expect(pinned).toMatch(/left:\s*0/);
    expect(SHEET).toMatch(/\.utable-scroll-hint\s*\{\s*display:\s*none/);
    expect(SHEET).toMatch(/@media\s*\(max-width:\s*560px\)[\s\S]*?\.utable-scroll-hint\s*\{\s*display:\s*inline/);
  });

  it("stacks cards and keeps range labels intact on a phone", () => {
    expect(SHEET).toMatch(/\.uranges button\s*\{[^}]*white-space:\s*nowrap/);
    expect(SHEET).toMatch(/@media\s*\(max-width:\s*560px\)[\s\S]*?\.utwo\s*\{\s*grid-template-columns:\s*1fr/);
  });

  it("stacks the custom date fields before they squeeze the inputs", () => {
    expect(SHEET).toMatch(/@media\s*\(max-width:\s*560px\)[\s\S]*?\.udates\s*\{[^}]*grid-template-columns:\s*1fr/);
  });
});
