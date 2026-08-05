export type CodeReadabilityMode = "light" | "dark";

export type CodeReadabilityPalette = {
  background: string;
  border: string;
  foreground: string;
  keyword: string;
  string: string;
  number: string;
  comment: string;
  function: string;
  type: string;
  builtin: string;
  meta: string;
  addition: string;
  deletion: string;
  additionBackground: string;
  deletionBackground: string;
};

type RGB = { r: number; g: number; b: number; a: number };

const MIN_CODE_CONTRAST = 4.5;
const DIFF_TINT_ALPHA = 0.12;

const BASE_SURFACES: Record<string, Record<CodeReadabilityMode, { bg: string; code: string; fg: string }>> = {
  graphite: {
    dark: { bg: "#0c0d10", code: "#101115", fg: "#f1f1ef" },
    light: { bg: "#f4f3ef", code: "#f6f5f1", fg: "#16181d" },
  },
  aurora: {
    dark: { bg: "#0e0d18", code: "#0f0e1c", fg: "#ecebf7" },
    light: { bg: "#f6f3fb", code: "#f3effb", fg: "#1c1830" },
  },
  slate: {
    dark: { bg: "#0d0f12", code: "#0c0e12", fg: "#e7eaf0" },
    light: { bg: "#f5f6f9", code: "#f1f3f7", fg: "#15181f" },
  },
  carbon: {
    dark: { bg: "#0e0d0c", code: "#0d0c0b", fg: "#ede9e3" },
    light: { bg: "#f6f4f0", code: "#f2efe9", fg: "#1a1714" },
  },
  nocturne: {
    dark: { bg: "#101019", code: "#0e0e18", fg: "#eceaf3" },
    light: { bg: "#f6f5fb", code: "#f2f0fb", fg: "#1b1830" },
  },
  amber: {
    dark: { bg: "#090a0c", code: "#111319", fg: "#f4f5f7" },
    light: { bg: "#f7f8fb", code: "#f2f5f9", fg: "#111827" },
  },
};

const DARK_SYNTAX = {
  keyword: "#c678dd",
  string: "#98c379",
  number: "#d19a66",
  comment: "#6a6a72",
  function: "#61afef",
  type: "#e5c07b",
  builtin: "#56b6c2",
  meta: "#7f848e",
  addition: "#74b87a",
  deletion: "#e0696a",
};

const LIGHT_SYNTAX = {
  keyword: "#cf222e",
  string: "#0a3069",
  number: "#0550ae",
  comment: "#6e7781",
  function: "#8250df",
  type: "#116329",
  builtin: "#0550ae",
  meta: "#6e7781",
  addition: "#15803d",
  deletion: "#dc2626",
};

/**
 * Build an opaque, WCAG-safe code surface from user-editable theme tokens.
 * Eight-digit colors are composited before use so a background image can never
 * leak through code or diff text.
 */
export function deriveCodeReadabilityPalette(
  mode: CodeReadabilityMode,
  baseStyle: string,
  tokens: Record<string, string> = {},
): CodeReadabilityPalette {
  const fallback = (BASE_SURFACES[baseStyle] || BASE_SURFACES.graphite)[mode];
  const baseBackground = flattenHex(tokens.bg || fallback.bg, fallback.bg);
  const background = flattenHex(tokens.bgSoft || fallback.code, baseBackground);
  const rawForeground = flattenHex(tokens.fg || fallback.fg, background);

  // Palette direction follows the final, opaque code surface rather than the
  // global theme switch. This covers intentionally inverted custom themes.
  const syntax = contrastRatio("#f1f1ef", background) >= contrastRatio("#111827", background)
    ? DARK_SYNTAX
    : LIGHT_SYNTAX;
  const rawAddition = flattenHex(tokens.ok || syntax.addition, background);
  const rawDeletion = flattenHex(tokens.err || syntax.deletion, background);
  const contrastTarget = preferredContrastTarget([background]);
  const additionBackground = readableTintBackground(background, rawAddition, contrastTarget);
  const deletionBackground = readableTintBackground(background, rawDeletion, contrastTarget);
  const renderedBackgrounds = [background, additionBackground, deletionBackground];
  const foreground = ensureContrastAgainst(rawForeground, renderedBackgrounds);
  const rawBorder = tokens.borderSoft || tokens.border;
  const border = rawBorder
    ? flattenHex(rawBorder, background)
    : mixHex(background, foreground, 0.16);

  return {
    background,
    border,
    foreground,
    keyword: ensureContrastAgainst(syntax.keyword, renderedBackgrounds),
    string: ensureContrastAgainst(syntax.string, renderedBackgrounds),
    number: ensureContrastAgainst(syntax.number, renderedBackgrounds),
    comment: ensureContrastAgainst(syntax.comment, renderedBackgrounds),
    function: ensureContrastAgainst(syntax.function, renderedBackgrounds),
    type: ensureContrastAgainst(syntax.type, renderedBackgrounds),
    builtin: ensureContrastAgainst(syntax.builtin, renderedBackgrounds),
    meta: ensureContrastAgainst(syntax.meta, renderedBackgrounds),
    addition: ensureContrastAgainst(rawAddition, renderedBackgrounds),
    deletion: ensureContrastAgainst(rawDeletion, renderedBackgrounds),
    additionBackground,
    deletionBackground,
  };
}

export function deriveCreationCodeReadabilityPalette(mode: CodeReadabilityMode): CodeReadabilityPalette {
  return deriveCodeReadabilityPalette(mode, "graphite", mode === "light"
    ? { bg: "#f6f4f1", bgSoft: "#f0eeeb", fg: "#18181b", borderSoft: "#18181b14" }
    : { bg: "#090c10", bgSoft: "#0b0e12", fg: "#e4e6ea", borderSoft: "#ffffff12" });
}

export function codeReadabilityDecls(palette: CodeReadabilityPalette): string {
  return [
    `--code-bg:${palette.background}`,
    `--code-border:${palette.border}`,
    `--code-fg:${palette.foreground}`,
    `--hl-keyword:${palette.keyword}`,
    `--hl-string:${palette.string}`,
    `--hl-number:${palette.number}`,
    `--hl-comment:${palette.comment}`,
    `--hl-func:${palette.function}`,
    `--hl-type:${palette.type}`,
    `--hl-builtin:${palette.builtin}`,
    `--hl-meta:${palette.meta}`,
    `--add-fg:${palette.addition}`,
    `--del-fg:${palette.deletion}`,
    `--code-add-bg:${palette.additionBackground}`,
    `--code-del-bg:${palette.deletionBackground}`,
  ].join(";");
}

export function codeReadabilityRatios(palette: CodeReadabilityPalette): Record<string, number> {
  const backgrounds = [palette.background, palette.additionBackground, palette.deletionBackground];
  return Object.fromEntries(
    ["foreground", "keyword", "string", "number", "comment", "function", "type", "builtin", "meta", "addition", "deletion"]
      .map((key) => {
        const foreground = palette[key as keyof CodeReadabilityPalette];
        return [key, Math.min(...backgrounds.map((background) => contrastRatio(foreground, background)))];
      }),
  );
}

/**
 * Install complete code palettes for every built-in style before any optional
 * theme-pack overlay. Creation intentionally uses its own neutral surface, so
 * it receives a complete local palette instead of overriding only --code-bg.
 */
export function baseCodeReadabilityStylesheet(styles: readonly string[]): string {
  const rules: string[] = [];
  for (const style of styles) {
    const darkDecls = codeReadabilityDecls(deriveCodeReadabilityPalette("dark", style));
    const lightDecls = codeReadabilityDecls(deriveCodeReadabilityPalette("light", style));
    rules.push(`:root[data-theme-style="${style}"]{${darkDecls}}`);
    rules.push(`:root[data-theme="light"][data-theme-style="${style}"]{${lightDecls}}`);
    rules.push(`@media (prefers-color-scheme: light){:root:not([data-theme])[data-theme-style="${style}"]{${lightDecls}}}`);
  }

  const creationDark = codeReadabilityDecls(deriveCreationCodeReadabilityPalette("dark"));
  const creationLight = codeReadabilityDecls(deriveCreationCodeReadabilityPalette("light"));
  rules.push(`:root[data-theme-style] .app--creation{${creationDark}}`);
  rules.push(`:root[data-theme="light"][data-theme-style] .app--creation{${creationLight}}`);
  rules.push(`@media (prefers-color-scheme: light){:root:not([data-theme])[data-theme-style] .app--creation{${creationLight}}}`);
  return rules.join("\n");
}

export function contrastRatio(a: string, b: string): number {
  const la = relativeLuminance(flattenHex(a, "#000000"));
  const lb = relativeLuminance(flattenHex(b, "#000000"));
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function ensureContrastAgainst(color: string, backgrounds: string[]): string {
  const primaryBackground = backgrounds[0] || "#000000";
  const opaque = flattenHex(color, primaryBackground);
  const minimumRatio = (candidate: string) => Math.min(
    ...backgrounds.map((background) => contrastRatio(candidate, background)),
  );
  if (minimumRatio(opaque) >= MIN_CODE_CONTRAST) return opaque;
  const target = preferredContrastTarget(backgrounds);
  let low = 0;
  let high = 1;
  for (let i = 0; i < 16; i += 1) {
    const middle = (low + high) / 2;
    if (minimumRatio(mixHex(opaque, target, middle)) >= MIN_CODE_CONTRAST) high = middle;
    else low = middle;
  }
  return mixHex(opaque, target, high);
}

function preferredContrastTarget(backgrounds: string[]): "#ffffff" | "#000000" {
  const minimumRatio = (candidate: string) => Math.min(
    ...backgrounds.map((background) => contrastRatio(candidate, background)),
  );
  return minimumRatio("#ffffff") >= minimumRatio("#000000") ? "#ffffff" : "#000000";
}

/**
 * Keep as much semantic diff tint as possible without crossing the contrast
 * direction selected by the opaque code surface. The zero-tint candidate is
 * always safe, so every syntax role can share one palette across plain, added,
 * and deleted rows instead of requiring row-specific token colors.
 */
function readableTintBackground(background: string, tint: string, foreground: string): string {
  const steps = 24;
  for (let step = steps; step >= 0; step -= 1) {
    const candidate = mixHex(background, tint, DIFF_TINT_ALPHA * (step / steps));
    if (contrastRatio(foreground, candidate) >= MIN_CODE_CONTRAST) return candidate;
  }
  return background;
}

function flattenHex(color: string, backdrop: string): string {
  const foreground = parseHex(color);
  const background = parseHex(backdrop);
  if (!foreground || !background) return "#000000";
  const alpha = foreground.a;
  return rgbToHex({
    r: foreground.r * alpha + background.r * (1 - alpha),
    g: foreground.g * alpha + background.g * (1 - alpha),
    b: foreground.b * alpha + background.b * (1 - alpha),
    a: 1,
  });
}

function mixHex(from: string, to: string, amount: number): string {
  const a = parseHex(from) || parseHex("#000000")!;
  const b = parseHex(to) || parseHex("#000000")!;
  const t = Math.min(1, Math.max(0, amount));
  return rgbToHex({
    r: a.r + (b.r - a.r) * t,
    g: a.g + (b.g - a.g) * t,
    b: a.b + (b.b - a.b) * t,
    a: 1,
  });
}

function parseHex(value: string): RGB | null {
  const match = /^#([0-9a-f]{6})([0-9a-f]{2})?$/i.exec(value.trim());
  if (!match) return null;
  const rgb = match[1];
  return {
    r: parseInt(rgb.slice(0, 2), 16),
    g: parseInt(rgb.slice(2, 4), 16),
    b: parseInt(rgb.slice(4, 6), 16),
    a: match[2] ? parseInt(match[2], 16) / 255 : 1,
  };
}

function rgbToHex(color: RGB): string {
  const channel = (value: number) => Math.round(Math.min(255, Math.max(0, value))).toString(16).padStart(2, "0");
  return `#${channel(color.r)}${channel(color.g)}${channel(color.b)}`;
}

function relativeLuminance(hex: string): number {
  const color = parseHex(hex) || parseHex("#000000")!;
  const channel = (value: number) => {
    const normalized = value / 255;
    return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
}
