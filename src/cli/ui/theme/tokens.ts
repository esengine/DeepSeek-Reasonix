export type ThemeName = "dark" | "light" | "midnight" | "deep-blue" | "high-contrast";

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
  messageBg: {
    user: string;
    bash: string;
    selected: string;
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
    bgInput: "#0f172a",
    bgCode: "#080c16",
    bgElev: "#151d2f",
  },
  messageBg: {
    user: "#373737",
    bash: "#413c41",
    selected: "#2c323e",
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
    bgInput: "#f1f5f9",
    bgCode: "#f3f4f6",
    bgElev: "#eef2f7",
  },
  messageBg: {
    user: "#e5e7eb",
    bash: "#f5e0e9",
    selected: "#dde6f5",
  },
});

const midnight = defineTheme({
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
  messageBg: {
    user: "#2a2d44",
    bash: "#39304a",
    selected: "#1f2740",
  },
});

const deepBlue = defineTheme({
  fg: {
    strong: "#ffffff",
    body: "#e0e0e0",
    sub: "#b0b0b0",
    meta: "#808080",
    faint: "#606060",
  },
  tone: {
    brand: "#0153e5",
    accent: "#4d94ff",
    violet: "#7b68ee",
    ok: "#4caf50",
    warn: "#ff9800",
    err: "#f44336",
    info: "#2196f3",
  },
  toneActive: {
    brand: "#4d94ff",
    accent: "#80b3ff",
    violet: "#9b8bff",
    ok: "#66bb6a",
    warn: "#ffb74d",
    err: "#ef5350",
    info: "#42a5f5",
  },
  surface: {
    bg: "#0a0a0a",
    bgInput: "#1e1e1e",
    bgCode: "#141414",
    bgElev: "#252525",
  },
  messageBg: {
    user: "#1c1c2a",
    bash: "#2a1f2a",
    selected: "#162033",
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
  messageBg: {
    user: "#1a1a1a",
    bash: "#241f24",
    selected: "#102030",
  },
});

export const THEMES = {
  dark,
  light,
  midnight,
  "deep-blue": deepBlue,
  "high-contrast": highContrast,
} as const satisfies Record<ThemeName, ThemeTokens>;

export const DEFAULT_THEME_NAME: ThemeName = "dark";

export function isThemeName(value: string): value is ThemeName {
  return Object.prototype.hasOwnProperty.call(THEMES, value);
}

export function resolveThemeName(value?: string | null): ThemeName {
  if (!value || value === "auto") return DEFAULT_THEME_NAME;
  // Handle old theme names
  if (value === "default" || value === "github-dark") return "dark";
  if (value === "github-light") return "light";
  if (value === "tokyo-night") return "midnight";
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
export const MESSAGE_BG = proxyTokens((theme) => theme.messageBg);
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
