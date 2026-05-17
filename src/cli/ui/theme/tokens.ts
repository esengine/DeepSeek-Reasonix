export type ThemeName =
  | "default"
  | "dark"
  | "light"
  | "tokyo-night"
  | "github-dark"
  | "github-light"
  | "high-contrast";

export interface ThemeTokens {
  fg: {
    strong: string;
    body: string;
    sub: string;
    meta: string;
    faint: string;
  };
  tone: {
    brand: string;
    accent: string;
    violet: string;
    ok: string;
    warn: string;
    err: string;
    info: string;
  };
  toneActive: ThemeTokens["tone"];
  surface: {
    bg: string;
    bgInput: string;
    bgCode: string;
    bgElev: string;
  };
  card: Record<
    | "user"
    | "reasoning"
    | "streaming"
    | "task"
    | "tool"
    | "plan"
    | "diff"
    | "error"
    | "warn"
    | "usage"
    | "subagent"
    | "approval"
    | "search"
    | "memory"
    | "ctx"
    | "doctor"
    | "branch",
    { color: string; glyph: string }
  >;
}

type ThemeBase = Omit<ThemeTokens, "card">;

function card(fg: ThemeTokens["fg"], tone: ThemeTokens["tone"]): ThemeTokens["card"] {
  return {
    user: { color: tone.brand, glyph: "◇" },
    reasoning: { color: tone.accent, glyph: "◆" },
    streaming: { color: tone.brand, glyph: "◈" },
    task: { color: tone.warn, glyph: "▶" },
    tool: { color: tone.info, glyph: "▣" },
    plan: { color: tone.accent, glyph: "⊞" },
    diff: { color: tone.ok, glyph: "±" },
    error: { color: tone.err, glyph: "✖" },
    warn: { color: tone.warn, glyph: "⚠" },
    usage: { color: fg.meta, glyph: "Σ" },
    subagent: { color: tone.violet, glyph: "⌬" },
    approval: { color: tone.warn, glyph: "?" },
    search: { color: tone.info, glyph: "⊙" },
    memory: { color: fg.meta, glyph: "⌑" },
    ctx: { color: tone.brand, glyph: "◔" },
    doctor: { color: fg.meta, glyph: "⚕" },
    branch: { color: tone.violet, glyph: "⎇" },
  };
}

function defineTheme(base: ThemeBase): ThemeTokens {
  return { ...base, card: card(base.fg, base.tone) };
}

const githubDark = defineTheme({
  fg: {
    strong: "#e6edf3",
    body: "#c9d1d9",
    sub: "#8b949e",
    meta: "#6e7681",
    faint: "#484f58",
  },
  tone: {
    brand: "#79c0ff",
    accent: "#d2a8ff",
    violet: "#b395f5",
    ok: "#7ee787",
    warn: "#f0b07d",
    err: "#ff8b81",
    info: "#79c0ff",
  },
  toneActive: {
    brand: "#a5d6ff",
    accent: "#e2c5ff",
    violet: "#c8aaff",
    ok: "#a8f5ad",
    warn: "#ffc99e",
    err: "#ffaba3",
    info: "#a5d6ff",
  },
  surface: {
    bg: "#0a0c10",
    bgInput: "#0d1015",
    bgCode: "#06080c",
    bgElev: "#11141a",
  },
});

const dark = defineTheme({
  fg: {
    strong: "#f4f7fb",
    body: "#d8dee9",
    sub: "#a7b1c2",
    meta: "#778294",
    faint: "#4d5666",
  },
  tone: {
    brand: "#7dd3fc",
    accent: "#c084fc",
    violet: "#a78bfa",
    ok: "#86efac",
    warn: "#fbbf24",
    err: "#f87171",
    info: "#60a5fa",
  },
  toneActive: {
    brand: "#bae6fd",
    accent: "#e9d5ff",
    violet: "#ddd6fe",
    ok: "#bbf7d0",
    warn: "#fde68a",
    err: "#fecaca",
    info: "#bfdbfe",
  },
  surface: {
    bg: "#0b1020",
    bgInput: "#111827",
    bgCode: "#080c16",
    bgElev: "#151d2f",
  },
});

const light = defineTheme({
  fg: {
    strong: "#111827",
    body: "#1f2937",
    sub: "#4b5563",
    meta: "#6b7280",
    faint: "#9ca3af",
  },
  tone: {
    brand: "#2563eb",
    accent: "#7c3aed",
    violet: "#6d28d9",
    ok: "#15803d",
    warn: "#b45309",
    err: "#dc2626",
    info: "#0369a1",
  },
  toneActive: {
    brand: "#1d4ed8",
    accent: "#6d28d9",
    violet: "#5b21b6",
    ok: "#166534",
    warn: "#92400e",
    err: "#b91c1c",
    info: "#075985",
  },
  surface: {
    bg: "#ffffff",
    bgInput: "#f8fafc",
    bgCode: "#f3f4f6",
    bgElev: "#eef2f7",
  },
});

const tokyoNight = defineTheme({
  fg: {
    strong: "#c0caf5",
    body: "#a9b1d6",
    sub: "#9aa5ce",
    meta: "#565f89",
    faint: "#414868",
  },
  tone: {
    brand: "#7aa2f7",
    accent: "#bb9af7",
    violet: "#9d7cd8",
    ok: "#9ece6a",
    warn: "#e0af68",
    err: "#f7768e",
    info: "#2ac3de",
  },
  toneActive: {
    brand: "#a9c7ff",
    accent: "#d7b9ff",
    violet: "#c6a0f6",
    ok: "#b9f27c",
    warn: "#ffd089",
    err: "#ff9cac",
    info: "#7dcfff",
  },
  surface: {
    bg: "#1a1b26",
    bgInput: "#1f2335",
    bgCode: "#16161e",
    bgElev: "#24283b",
  },
});

const githubLight = defineTheme({
  fg: {
    strong: "#1f2328",
    body: "#24292f",
    sub: "#57606a",
    meta: "#6e7781",
    faint: "#8c959f",
  },
  tone: {
    brand: "#0969da",
    accent: "#8250df",
    violet: "#6639ba",
    ok: "#1a7f37",
    warn: "#9a6700",
    err: "#cf222e",
    info: "#0969da",
  },
  toneActive: {
    brand: "#0550ae",
    accent: "#6639ba",
    violet: "#512a97",
    ok: "#116329",
    warn: "#7d4e00",
    err: "#a40e26",
    info: "#0550ae",
  },
  surface: {
    bg: "#ffffff",
    bgInput: "#f6f8fa",
    bgCode: "#f6f8fa",
    bgElev: "#eaeef2",
  },
});

const highContrast = defineTheme({
  fg: {
    strong: "#ffffff",
    body: "#f5f5f5",
    sub: "#d4d4d4",
    meta: "#bdbdbd",
    faint: "#8a8a8a",
  },
  tone: {
    brand: "#00e5ff",
    accent: "#ff4dff",
    violet: "#b388ff",
    ok: "#00ff66",
    warn: "#ffdd00",
    err: "#ff4d4d",
    info: "#4da3ff",
  },
  toneActive: {
    brand: "#80f2ff",
    accent: "#ff99ff",
    violet: "#d0b3ff",
    ok: "#80ffb3",
    warn: "#ffee80",
    err: "#ff9999",
    info: "#99c9ff",
  },
  surface: {
    bg: "#000000",
    bgInput: "#0a0a0a",
    bgCode: "#050505",
    bgElev: "#141414",
  },
});

export const THEMES = {
  default: githubDark,
  dark,
  light,
  "tokyo-night": tokyoNight,
  "github-dark": githubDark,
  "github-light": githubLight,
  "high-contrast": highContrast,
} as const satisfies Record<ThemeName, ThemeTokens>;

export const DEFAULT_THEME_NAME: ThemeName = "default";

export function isThemeName(value: string): value is ThemeName {
  return Object.prototype.hasOwnProperty.call(THEMES, value);
}

export function resolveThemeName(value?: string | null): ThemeName {
  if (!value || value === "auto") return DEFAULT_THEME_NAME;
  return isThemeName(value) ? value : DEFAULT_THEME_NAME;
}

export function listThemeNames(): ThemeName[] {
  return Object.keys(THEMES) as ThemeName[];
}

export function themeTokens(name?: string | null): ThemeTokens {
  return THEMES[resolveThemeName(name)];
}

export const DEFAULT_THEME = THEMES[DEFAULT_THEME_NAME];

let activeTheme: ThemeTokens = DEFAULT_THEME;
let activeThemeVersion = 0;

export function setActiveTheme(theme: ThemeTokens): () => void {
  const previousTheme = activeTheme;
  activeTheme = theme;
  activeThemeVersion += 1;
  const version = activeThemeVersion;
  return () => {
    if (activeThemeVersion !== version || activeTheme !== theme) return;
    activeTheme = previousTheme;
    activeThemeVersion += 1;
  };
}

function proxyTokens<T extends object>(select: (theme: ThemeTokens) => T): T {
  const target = select(DEFAULT_THEME);
  return new Proxy(target, {
    get(_target, prop: string | symbol) {
      return select(activeTheme)[prop as keyof T];
    },
    getOwnPropertyDescriptor(_target, prop: string | symbol) {
      return Reflect.getOwnPropertyDescriptor(select(activeTheme), prop);
    },
    has(_target, prop: string | symbol) {
      return prop in select(activeTheme);
    },
    ownKeys() {
      return Reflect.ownKeys(select(activeTheme));
    },
  });
}

export const FG = proxyTokens((theme) => theme.fg);
export const TONE = proxyTokens((theme) => theme.tone);
export const TONE_ACTIVE = proxyTokens((theme) => theme.toneActive);
export const SURFACE = proxyTokens((theme) => theme.surface);
export const CARD = proxyTokens((theme) => theme.card);

export type CardTone = keyof ThemeTokens["card"];

/** DeepSeek prices in CNY; our internal table is USD divided by 7.2. Multiply back for display. */
export const USD_TO_CNY = 7.2;

const SYMBOL: Record<string, string> = { USD: "$", CNY: "¥" };

/** Format an amount already in `currency`. Undefined currency → CNY (matches pre-fix behavior). */
export function formatBalance(
  amount: number,
  currency?: string,
  opts?: { fractionDigits?: number; label?: boolean },
): string {
  const cur = currency ?? "CNY";
  const sym = SYMBOL[cur];
  const digits = opts?.fractionDigits ?? 2;
  const body = sym ? `${sym}${amount.toFixed(digits)}` : `${cur} ${amount.toFixed(digits)}`;
  return opts?.label ? `w ${body}` : body;
}

/** Format an internal USD cost in the wallet's display currency. Undefined currency → CNY. */
export function formatCost(costUsd: number, currency?: string, fractionDigits = 4): string {
  const cur = currency ?? "CNY";
  const amount = cur === "CNY" ? costUsd * USD_TO_CNY : costUsd;
  return formatBalance(amount, cur, { fractionDigits });
}

/** Threshold color for a wallet balance. USD is converted to CNY before the threshold check. */
export function balanceColor(amount: number, currency?: string): string {
  const cny = (currency ?? "CNY") === "USD" ? amount * USD_TO_CNY : amount;
  if (cny < 5) return TONE.err;
  if (cny < 20) return TONE.warn;
  return TONE.brand;
}
