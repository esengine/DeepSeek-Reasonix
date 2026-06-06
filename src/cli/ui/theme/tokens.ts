import type { Color } from "ink";

export type PublicThemeName =
  | "graphite"
  | "ember"
  | "aurora"
  | "sandstone"
  | "porcelain"
  | "linen"
  | "glacier"
  | "midnight";

type LegacyThemeName = "dark" | "light" | "deep-blue" | "high-contrast";

export type ThemeName = PublicThemeName | LegacyThemeName;

export interface ThemeTokens {
  fg: {
    strong: Color;
    body: Color;
    sub: Color;
    meta: Color;
    faint: Color;
  };
  tone: {
    brand: Color;
    accent: Color;
    violet: Color;
    ok: Color;
    warn: Color;
    err: Color;
    info: Color;
  };
  toneActive: ThemeTokens["tone"];
  surface: {
    bg: Color;
    bgInput: Color;
    bgCode: Color;
    bgElev: Color;
  };
  messageBg: {
    user: Color;
    bash: Color;
    selected: Color;
  };
  pill: {
    bg: Color;
    section: Record<
      | "reason"
      | "output"
      | "tool"
      | "shell"
      | "task"
      | "taskDone"
      | "taskFailed"
      | "plan"
      | "user"
      | "empty",
      { bg: Color; fg: Color }
    >;
    path: { bg: Color; fg: Color };
    model: Record<"flash" | "pro" | "r1" | "unknown", { bg: Color; fg: Color }>;
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
    { color: Color; glyph: string }
  >;
}

type ThemeBase = Omit<ThemeTokens, "card" | "pill">;

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

function pill(
  surface: ThemeTokens["surface"],
  tone: ThemeTokens["tone"],
  fg: ThemeTokens["fg"],
): ThemeTokens["pill"] {
  const bg = surface.bgElev;
  return {
    bg,
    section: {
      reason: { bg, fg: tone.violet },
      output: { bg, fg: tone.info },
      tool: { bg, fg: tone.info },
      shell: { bg, fg: tone.info },
      task: { bg, fg: tone.info },
      taskDone: { bg, fg: tone.ok },
      taskFailed: { bg, fg: tone.err },
      plan: { bg, fg: tone.violet },
      user: { bg, fg: tone.brand },
      empty: { bg, fg: fg.faint },
    },
    path: { bg, fg: fg.meta },
    model: {
      flash: { bg, fg: tone.info },
      pro: { bg, fg: tone.violet },
      r1: { bg, fg: tone.accent },
      unknown: { bg, fg: fg.meta },
    },
  };
}

function defineTheme(base: ThemeBase): ThemeTokens {
  return { ...base, card: card(base.fg, base.tone), pill: pill(base.surface, base.tone, base.fg) };
}

const dark = defineTheme({
  fg: {
    strong: "#f4f7fb",
    body: "#d8dee9",
    sub: "#a7b1c2",
    meta: "#9aa5b5",
    faint: "#8791a3",
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
    bgElev: "#1c2844",
  },
  messageBg: {
    user: "#373737",
    bash: "#413c41",
    selected: "#2c323e",
  },
});

const graphite = dark;

const light = defineTheme({
  fg: {
    strong: "#111827",
    body: "#1f2937",
    sub: "#4b5563",
    meta: "#5c6371",
    faint: "#666d7b",
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

const porcelain = light;

const midnight = defineTheme({
  fg: {
    strong: "#c0caf5",
    body: "#a9b1d6",
    sub: "#9aa5ce",
    meta: "#9da5bb",
    faint: "#8a92a8",
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
    meta: "#909090",
    faint: "#8c8c8c",
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

const ember = defineTheme({
  fg: {
    strong: "#fff7ed",
    body: "#f5dec8",
    sub: "#c8ad96",
    meta: "#947a66",
    faint: "#665142",
  },
  tone: {
    brand: "#fb923c",
    accent: "#fdba74",
    violet: "#f0abfc",
    ok: "#86efac",
    warn: "#facc15",
    err: "#fb7185",
    info: "#7dd3fc",
  },
  toneActive: {
    brand: "#fed7aa",
    accent: "#ffedd5",
    violet: "#f5d0fe",
    ok: "#bbf7d0",
    warn: "#fef08a",
    err: "#fecdd3",
    info: "#bae6fd",
  },
  surface: {
    bg: "#140f0b",
    bgInput: "#1f160f",
    bgCode: "#0f0a07",
    bgElev: "#2a1d13",
  },
  messageBg: {
    user: "#342016",
    bash: "#2b1c18",
    selected: "#3a261a",
  },
});

const aurora = defineTheme({
  fg: {
    strong: "#ecfeff",
    body: "#d1fae5",
    sub: "#a7c9bf",
    meta: "#73958c",
    faint: "#4d655f",
  },
  tone: {
    brand: "#5eead4",
    accent: "#93c5fd",
    violet: "#c4b5fd",
    ok: "#86efac",
    warn: "#fbbf24",
    err: "#fb7185",
    info: "#67e8f9",
  },
  toneActive: {
    brand: "#99f6e4",
    accent: "#bfdbfe",
    violet: "#ddd6fe",
    ok: "#bbf7d0",
    warn: "#fde68a",
    err: "#fecdd3",
    info: "#a5f3fc",
  },
  surface: {
    bg: "#071817",
    bgInput: "#0b2422",
    bgCode: "#061211",
    bgElev: "#12302d",
  },
  messageBg: {
    user: "#12352f",
    bash: "#142c34",
    selected: "#163b3a",
  },
});

const sandstone = defineTheme({
  fg: {
    strong: "#24180f",
    body: "#38291e",
    sub: "#6a5848",
    meta: "#806b58",
    faint: "#ad9982",
  },
  tone: {
    brand: "#c05621",
    accent: "#b45309",
    violet: "#7c3aed",
    ok: "#15803d",
    warn: "#b45309",
    err: "#dc2626",
    info: "#0369a1",
  },
  toneActive: {
    brand: "#9a3412",
    accent: "#92400e",
    violet: "#6d28d9",
    ok: "#166534",
    warn: "#92400e",
    err: "#b91c1c",
    info: "#075985",
  },
  surface: {
    bg: "#fff4e3",
    bgInput: "#f3e3cc",
    bgCode: "#fbedd7",
    bgElev: "#ead7bd",
  },
  messageBg: {
    user: "#ead7bd",
    bash: "#efdfd0",
    selected: "#dfc7a8",
  },
});

const linen = defineTheme({
  fg: {
    strong: "#1f1a14",
    body: "#312820",
    sub: "#5a4a3c",
    meta: "#766554",
    faint: "#a38f79",
  },
  tone: {
    brand: "#c05621",
    accent: "#2563eb",
    violet: "#7c3aed",
    ok: "#15803d",
    warn: "#b45309",
    err: "#dc2626",
    info: "#0369a1",
  },
  toneActive: {
    brand: "#9a3412",
    accent: "#1d4ed8",
    violet: "#6d28d9",
    ok: "#166534",
    warn: "#92400e",
    err: "#b91c1c",
    info: "#075985",
  },
  surface: {
    bg: "#fff8ed",
    bgInput: "#f4eadb",
    bgCode: "#fbf0df",
    bgElev: "#eadcc9",
  },
  messageBg: {
    user: "#eadcc9",
    bash: "#f0dfd9",
    selected: "#e4d3bd",
  },
});

const glacier = defineTheme({
  fg: {
    strong: "#172033",
    body: "#25334a",
    sub: "#4f647f",
    meta: "#6b7f99",
    faint: "#9aacbf",
  },
  tone: {
    brand: "#0284c7",
    accent: "#2563eb",
    violet: "#7c3aed",
    ok: "#15803d",
    warn: "#b45309",
    err: "#dc2626",
    info: "#0369a1",
  },
  toneActive: {
    brand: "#0369a1",
    accent: "#1d4ed8",
    violet: "#6d28d9",
    ok: "#166534",
    warn: "#92400e",
    err: "#b91c1c",
    info: "#075985",
  },
  surface: {
    bg: "#f4faff",
    bgInput: "#eaf4fb",
    bgCode: "#f8fbff",
    bgElev: "#dceaf5",
  },
  messageBg: {
    user: "#dceaf5",
    bash: "#e8eef7",
    selected: "#d2e3f0",
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
  graphite,
  ember,
  aurora,
  sandstone,
  porcelain,
  linen,
  glacier,
  midnight,
  dark,
  light,
  "deep-blue": deepBlue,
  "high-contrast": highContrast,
} as const satisfies Record<ThemeName, ThemeTokens>;

export const THEME_NAMES = [
  "graphite",
  "ember",
  "aurora",
  "sandstone",
  "porcelain",
  "linen",
  "glacier",
  "midnight",
] as const satisfies readonly PublicThemeName[];

export const DEFAULT_THEME_NAME: PublicThemeName = "graphite";

export function isThemeName(value: string): value is ThemeName {
  return Object.prototype.hasOwnProperty.call(THEMES, value);
}

export function resolveThemeName(value?: string | null): ThemeName {
  if (!value || value === "auto") return DEFAULT_THEME_NAME;
  // Handle old theme names
  if (value === "default" || value === "github-dark" || value === "dark") return "graphite";
  if (value === "github-light" || value === "light") return "porcelain";
  if (value === "tokyo-night") return "midnight";
  return isThemeName(value) ? value : DEFAULT_THEME_NAME;
}

export function listThemeNames(): PublicThemeName[] {
  return [...THEME_NAMES];
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
export const PILL = proxyTokens((theme) => theme.pill);

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
export function balanceColor(amount: number, currency?: string): Color {
  const cny = (currency ?? "CNY") === "USD" ? amount * USD_TO_CNY : amount;
  if (cny < 5) return TONE.err;
  if (cny < 20) return TONE.warn;
  return TONE.brand;
}
