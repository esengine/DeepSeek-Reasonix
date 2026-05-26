import React from "react";
import { useThemeTokens } from "./theme/context.js";
import {
  CARD,
  FG as TOKEN_FG,
  MESSAGE_BG as TOKEN_MESSAGE_BG,
  SURFACE as TOKEN_SURFACE,
  TONE,
  TONE_ACTIVE,
  type ThemeTokens,
} from "./theme/tokens.js";

export type UiColor = ReturnType<typeof colorFromTheme>;
export type UiGradient = ReturnType<typeof gradientFromTheme>;
export type UiSurface = ReturnType<typeof surfaceFromTheme>;
export type UiFg = ReturnType<typeof fgFromTheme>;

export function gradientFromTheme(theme: ThemeTokens): ReadonlyArray<string> {
  return [
    theme.tone.ok,
    theme.tone.brand,
    theme.tone.info,
    theme.toneActive.brand,
    theme.toneActive.violet,
    theme.tone.accent,
    theme.toneActive.accent,
    theme.tone.err,
  ];
}

export function colorFromTheme(theme: ThemeTokens) {
  return {
    primary: theme.tone.brand,
    accent: theme.tone.accent,
    brand: theme.tone.ok,

    user: theme.tone.brand,
    assistant: theme.tone.ok,
    tool: theme.tone.warn,
    toolErr: theme.tone.err,
    info: theme.fg.sub,
    warn: theme.tone.warn,
    err: theme.tone.err,
    ok: theme.tone.ok,
  } as const;
}

export function surfaceFromTheme(theme: ThemeTokens) {
  return {
    canvas: theme.surface.bg,
    shell: theme.surface.bgInput,
    card: theme.surface.bgElev,
    elev: theme.surface.bgElev,
    sel: theme.surface.bgInput,
    line: theme.fg.faint,
    lineSoft: theme.fg.meta,
  } as const;
}

export function fgFromTheme(theme: ThemeTokens) {
  return {
    strong: theme.fg.strong,
    default: theme.fg.body,
    dim: theme.fg.sub,
    faint: theme.fg.meta,
    ghost: theme.fg.faint,
  } as const;
}

function proxyThemeValue<T extends object>(build: () => T): T {
  const target = build();
  return new Proxy(target, {
    get(_target, prop: string | symbol) {
      return build()[prop as keyof T];
    },
    getOwnPropertyDescriptor(_target, prop: string | symbol) {
      return Reflect.getOwnPropertyDescriptor(build(), prop);
    },
    has(_target, prop: string | symbol) {
      return prop in build();
    },
    ownKeys() {
      return Reflect.ownKeys(build());
    },
  });
}

function currentTheme(): ThemeTokens {
  return {
    fg: TOKEN_FG,
    tone: TONE,
    toneActive: TONE_ACTIVE,
    surface: TOKEN_SURFACE,
    messageBg: TOKEN_MESSAGE_BG,
    card: CARD,
  };
}

export function useGradient(): UiGradient {
  const theme = useThemeTokens();
  return React.useMemo(() => gradientFromTheme(theme), [theme]);
}

export function useColor(): UiColor {
  const theme = useThemeTokens();
  return React.useMemo(() => colorFromTheme(theme), [theme]);
}

export function useUiSurface(): UiSurface {
  const theme = useThemeTokens();
  return React.useMemo(() => surfaceFromTheme(theme), [theme]);
}

export function useUiFg(): UiFg {
  const theme = useThemeTokens();
  return React.useMemo(() => fgFromTheme(theme), [theme]);
}

export const GRADIENT = proxyThemeValue(() => gradientFromTheme(currentTheme()));
export const COLOR = proxyThemeValue(() => colorFromTheme(currentTheme()));

export const GLYPH = {
  brand: "●",
  user: "●",
  assistant: "●",
  toolOk: "✓",
  toolErr: "✗",
  warn: "⚠",
  err: "✗",
  arrow: "▸",
  bullet: "·",
  bar: "│",
  thinBar: "│",
  block: "█",
  shade1: "░",
  shade2: "▒",
  shade3: "▓",

  done: "✓",
  cur: "▸",
  pending: "○",
  fail: "✗",
  running: "●",

  branch: "├",
  branchEnd: "└",
  branchStub: "│",
  rule: "─",

  spinFrames: ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"] as readonly string[],
} as const;

export const SURFACE = proxyThemeValue(() => surfaceFromTheme(currentTheme()));
export const FG = proxyThemeValue(() => fgFromTheme(currentTheme()));

export function gradientCells(
  width: number,
  glyph: string = GLYPH.block,
  gradient: ReadonlyArray<string> = GRADIENT,
): Array<{ ch: string; color: string }> {
  const cells: Array<{ ch: string; color: string }> = [];
  if (width <= 0) return cells;
  const last = gradient.length - 1;
  for (let i = 0; i < width; i++) {
    if (last <= 0) {
      cells.push({ ch: glyph, color: gradient[0] ?? COLOR.primary });
      continue;
    }
    const t = width === 1 ? 0 : (i * last) / (width - 1);
    const lo = Math.floor(t);
    const hi = Math.min(last, lo + 1);
    const color = t - lo < 0.5 ? gradient[lo]! : gradient[hi]!;
    cells.push({ ch: glyph, color });
  }
  return cells;
}
