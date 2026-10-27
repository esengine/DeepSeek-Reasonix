import { useSyncExternalStore } from "react";

// Per-machine interface choices. They live in localStorage rather than in the
// kernel's settings because they govern this screen's presentation and input,
// not session facts, and do not travel with the session.

// On unless this machine turned it off. The desktop shell reads the same key
// from its saved preferences, so a flip here reaches it without a request.
const KEEP_AWAKE_KEY = "rx-keep-awake";

export function keepsAwake(): boolean {
  try {
    return localStorage.getItem(KEEP_AWAKE_KEY) !== "off";
  } catch {
    return true;
  }
}

export function setKeepsAwake(on: boolean): void {
  try {
    localStorage.setItem(KEEP_AWAKE_KEY, on ? "on" : "off");
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
}

// Off unless this machine turned it on. The balance and the session's cost are
// what a shared screen or a recording leaks; masking changes only what is drawn,
// so the reads behind them keep running and revealing shows the current value.
const AMOUNTS_KEY = "rx-hide-amounts";

let amountsHidden: boolean | null = null;
const amountListeners = new Set<() => void>();

export function hidesAmounts(): boolean {
  if (amountsHidden === null) {
    try {
      amountsHidden = localStorage.getItem(AMOUNTS_KEY) === "on";
    } catch {
      amountsHidden = false;
    }
  }
  return amountsHidden;
}

export function onHidesAmountsChange(fn: () => void): () => void {
  amountListeners.add(fn);
  return () => {
    amountListeners.delete(fn);
  };
}

export function setHidesAmounts(on: boolean): void {
  amountsHidden = on;
  try {
    localStorage.setItem(AMOUNTS_KEY, on ? "on" : "off");
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  amountListeners.forEach((fn) => fn());
}

// Off unless this machine turned it on. The desktop shell reads the same key
// from its saved preferences before Chromium starts, so a flip reaches it on
// the next launch and not before.
const SOFTWARE_KEY = "rx-hw-accel";

export function wantsSoftwareRendering(): boolean {
  try {
    return localStorage.getItem(SOFTWARE_KEY) === "off";
  } catch {
    return false;
  }
}

const softwareListeners = new Set<() => void>();

export function onSoftwareRenderingChange(fn: () => void): () => void {
  softwareListeners.add(fn);
  return () => {
    softwareListeners.delete(fn);
  };
}

export function setSoftwareRendering(on: boolean): void {
  try {
    localStorage.setItem(SOFTWARE_KEY, on ? "off" : "on");
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  softwareListeners.forEach((fn) => fn());
}

// "reduced" drops the layers that cost the GPU a frame each time the page
// moves: frosted glass, the animated sky, the grain. Anything else is "full".
const EFFECTS_KEY = "rx-effects";

export type EffectsMode = "full" | "reduced";

const effectsListeners = new Set<() => void>();

export function effectsMode(): EffectsMode {
  try {
    return localStorage.getItem(EFFECTS_KEY) === "reduced" ? "reduced" : "full";
  } catch {
    return "full";
  }
}

export function onEffectsChange(fn: () => void): () => void {
  effectsListeners.add(fn);
  return () => {
    effectsListeners.delete(fn);
  };
}

export function setEffectsMode(mode: EffectsMode): void {
  try {
    localStorage.setItem(EFFECTS_KEY, mode);
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  effectsListeners.forEach((fn) => fn());
}

// When the icon column beside the workspace rail is drawn: "on" always, "collapsed"
// only while the workspace rail is closed, "off" never. A phone-width window
// never draws it, whatever this says. "on" and "off" are the two values the
// earlier on/off switch wrote, so a stored choice reads back as itself; anything
// unreadable or unset is the default, "on".
const NAV_RAIL_KEY = "rx-nav-rail";

export type NavRailMode = "on" | "collapsed" | "off";
export const NAV_RAIL_MODES: readonly NavRailMode[] = ["on", "collapsed", "off"];

const navRailListeners = new Set<() => void>();

export function navRailMode(): NavRailMode {
  try {
    const raw = localStorage.getItem(NAV_RAIL_KEY);
    return raw === "collapsed" || raw === "off" ? raw : "on";
  } catch {
    return "on";
  }
}

export function onNavRailChange(fn: () => void): () => void {
  navRailListeners.add(fn);
  return () => {
    navRailListeners.delete(fn);
  };
}

export function setNavRailMode(mode: NavRailMode): void {
  try {
    localStorage.setItem(NAV_RAIL_KEY, mode);
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  navRailListeners.forEach((fn) => fn());
}

// How long a session that finished its turn stays in the sidebar's running list,
// in minutes. Off (0) unless this machine chose a value: leaving the list the
// moment a turn ends is the long-standing behaviour, and keeping a row there is
// the reader's call. Only the offered values are read back.
const LIVE_GRACE_KEY = "rx-live-grace";
export const LIVE_GRACE_CHOICES: readonly number[] = [0, 10, 30, 60, 240];

const liveGraceListeners = new Set<() => void>();

export function liveGraceMinutes(): number {
  try {
    const raw = localStorage.getItem(LIVE_GRACE_KEY);
    const minutes = raw === null ? 0 : Number(raw);
    return LIVE_GRACE_CHOICES.includes(minutes) && String(minutes) === raw ? minutes : 0;
  } catch {
    return 0;
  }
}

export function onLiveGraceChange(fn: () => void): () => void {
  liveGraceListeners.add(fn);
  return () => {
    liveGraceListeners.delete(fn);
  };
}

export function setLiveGraceMinutes(minutes: number): void {
  try {
    localStorage.setItem(LIVE_GRACE_KEY, String(minutes));
  } catch {
    /* a private window keeps the default, which is the same answer it gives */
  }
  liveGraceListeners.forEach((fn) => fn());
}

// Expansion defaults apply until the reader opens or closes a block.
export type Fold = "thinking" | "activity" | "steps" | "output" | "compaction";
export type FoldMode = "folded" | "live" | "failed" | "changed" | "open";
export type FoldModes = Readonly<Record<Fold, FoldMode>>;

export const FOLD_CHOICES: Readonly<Record<Fold, readonly FoldMode[]>> = {
  thinking: ["folded", "live", "open"],
  activity: ["folded", "live", "changed", "open"],
  steps: ["folded", "failed", "changed", "open"],
  output: ["folded", "open"],
  compaction: ["folded", "open"],
};

export const FOLD_DEFAULTS: FoldModes = {
  thinking: "folded",
  activity: "live",
  steps: "failed",
  output: "folded",
  compaction: "folded",
};

const FOLD_KEY = "rx-transcript-fold";
type FoldPreferences = { modes: FoldModes; thinkingPreview: boolean };

function readFolds(): FoldPreferences {
  let saved: Record<string, unknown> = {};
  try {
    const raw = JSON.parse(localStorage.getItem(FOLD_KEY) ?? "{}");
    if (raw && typeof raw === "object") saved = raw;
  } catch {
    /* unreadable or absent storage reads as the defaults */
  }
  const out = { ...FOLD_DEFAULTS };
  for (const kind of Object.keys(FOLD_CHOICES) as Fold[]) {
    const v = saved[kind];
    if (typeof v === "string" && (FOLD_CHOICES[kind] as readonly string[]).includes(v)) out[kind] = v as FoldMode;
  }
  return { modes: out, thinkingPreview: saved.thinkingPreview === true || saved.thinking === "peek" };
}

let folds: FoldPreferences | null = null;
const foldListeners = new Set<() => void>();

function foldPreferences(): FoldPreferences {
  folds ??= readFolds();
  return folds;
}

export function foldModes(): FoldModes {
  return foldPreferences().modes;
}

export function showsThinkingPreview(): boolean {
  return foldPreferences().thinkingPreview;
}

export function onFoldModesChange(fn: () => void): () => void {
  foldListeners.add(fn);
  return () => {
    foldListeners.delete(fn);
  };
}

export function setFoldModes(patch: Partial<FoldModes>): void {
  const next = { ...foldModes(), ...patch };
  for (const kind of Object.keys(patch) as Fold[]) {
    if (!FOLD_CHOICES[kind].includes(next[kind])) next[kind] = FOLD_DEFAULTS[kind];
  }
  saveFolds({ ...foldPreferences(), modes: next });
}

export function setShowsThinkingPreview(on: boolean): void {
  saveFolds({ ...foldPreferences(), thinkingPreview: on });
}

function saveFolds(next: FoldPreferences): void {
  folds = next;
  try {
    localStorage.setItem(FOLD_KEY, JSON.stringify({ ...next.modes, thinkingPreview: next.thinkingPreview }));
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  foldListeners.forEach((fn) => fn());
}

// A list of names this window remembers: the one place the read-through cache,
// the write, the cross-window refresh and the change listeners are written, so
// each list preference is one declaration rather than another copy of them.
function stringListPref(key: string) {
  let cachedRaw: string | null | undefined;
  let cached: readonly string[] = [];
  const listeners = new Set<() => void>();

  const read = (): readonly string[] => {
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(key);
    } catch {
      /* storage is unavailable; keep the default */
    }
    if (raw !== cachedRaw) {
      cachedRaw = raw;
      try {
        cached = normalizeNames(JSON.parse(raw ?? "[]"));
      } catch {
        cached = [];
      }
    }
    return cached;
  };
  const write = (list: readonly string[]): boolean => {
    const next = normalizeNames(list);
    const raw = JSON.stringify(next);
    try {
      localStorage.setItem(key, raw);
    } catch {
      return false;
    }
    if (raw !== cachedRaw) {
      cachedRaw = raw;
      cached = next;
      listeners.forEach((fn) => fn());
    }
    return true;
  };
  const refresh = (event: StorageEvent): void => {
    if (event.key !== key && event.key !== null) return;
    const before = cachedRaw;
    cachedRaw = undefined;
    read();
    if (cachedRaw !== before) listeners.forEach((fn) => fn());
  };
  const on = (fn: () => void): (() => void) => {
    listeners.add(fn);
    if (listeners.size === 1) window.addEventListener("storage", refresh);
    return () => {
      listeners.delete(fn);
      if (listeners.size === 0) window.removeEventListener("storage", refresh);
    };
  };
  return { read, write, on };
}

function normalizeNames(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((item): item is string => typeof item === "string" && item.trim() !== "").map((item) => item.trim()))];
}

// Service order is a display choice, independent of the provider configuration.
export const PROVIDER_ORDER_KEY = "rx-provider-order";
const providerOrder = stringListPref(PROVIDER_ORDER_KEY);
export const normalizeProviderOrder = normalizeNames;
export const readProviderOrder = providerOrder.read;
export const writeProviderOrder = providerOrder.write;
export const onProviderOrderChange = providerOrder.on;

// Which accounts the model menu is narrowed to; empty shows them all. Keyed by
// the account's own key, so a removed account simply stops matching.
export const MODEL_FILTER_KEY = "rx-model-filter";
const modelFilter = stringListPref(MODEL_FILTER_KEY);
export const readModelFilter = modelFilter.read;
export const writeModelFilter = modelFilter.write;
export const onModelFilterChange = modelFilter.on;

// Off unless this machine turned it on. Dot folders hold tool state and local
// configuration, so the explorer lists them only for a reader who asked.
const HIDDEN_FILES_KEY = "rx-show-hidden-files";

export function showsHiddenFiles(): boolean {
  try {
    return localStorage.getItem(HIDDEN_FILES_KEY) === "on";
  } catch {
    return false;
  }
}

export function setShowsHiddenFiles(on: boolean): void {
  try {
    localStorage.setItem(HIDDEN_FILES_KEY, on ? "on" : "off");
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
}

// Whether sessions Reasonix 1.x wrote stay out of the sidebar lists. Off unless
// this machine turned it on; the lists show everything otherwise.
const HIDE_LEGACY_KEY = "rx-hide-legacy-sessions";
const hideLegacyListeners = new Set<() => void>();

export function hidesLegacySessions(): boolean {
  try {
    return localStorage.getItem(HIDE_LEGACY_KEY) === "on";
  } catch {
    return false;
  }
}

export function setHidesLegacySessions(on: boolean): void {
  try {
    localStorage.setItem(HIDE_LEGACY_KEY, on ? "on" : "off");
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  hideLegacyListeners.forEach((fn) => fn());
}

export function onHidesLegacySessionsChange(fn: () => void): () => void {
  hideLegacyListeners.add(fn);
  const storage = (event: StorageEvent) => {
    if (event.key === HIDE_LEGACY_KEY || event.key === null) fn();
  };
  window.addEventListener("storage", storage);
  return () => {
    hideLegacyListeners.delete(fn);
    window.removeEventListener("storage", storage);
  };
}

// How many sessions stay open as tabs before the oldest idle one is folded away.
// Zero is never: folding closes a pane on its own, so it is something the person
// asks for. Whatever is missing, corrupt or out of range reads as zero too.
const OPEN_SESSION_LIMIT_KEY = "rx-open-session-limit";
const openSessionLimitListeners = new Set<() => void>();

export function openSessionLimit(): number {
  try {
    const raw = localStorage.getItem(OPEN_SESSION_LIMIT_KEY);
    if (raw === null || !/^(0|[1-9][0-9]*)$/.test(raw)) return 0;
    const n = Number(raw);
    return Number.isSafeInteger(n) ? n : 0;
  } catch {
    return 0;
  }
}

export function setOpenSessionLimit(n: number): void {
  try {
    if (Number.isSafeInteger(n) && n > 0) localStorage.setItem(OPEN_SESSION_LIMIT_KEY, String(n));
    else localStorage.removeItem(OPEN_SESSION_LIMIT_KEY);
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  openSessionLimitListeners.forEach((fn) => fn());
}

export function onOpenSessionLimitChange(fn: () => void): () => void {
  openSessionLimitListeners.add(fn);
  const storage = (event: StorageEvent) => {
    if (event.key === OPEN_SESSION_LIMIT_KEY || event.key === null) fn();
  };
  window.addEventListener("storage", storage);
  return () => {
    openSessionLimitListeners.delete(fn);
    window.removeEventListener("storage", storage);
  };
}

// The send key is local to this machine, never read from project settings.
export type SendShortcut = "enter" | "modifier_enter";
const SEND_SHORTCUT_KEY = "rx-send-shortcut";
const sendShortcutListeners = new Set<() => void>();
let transientSendShortcut: SendShortcut = "enter";
// A refused write can still leave reads returning the old choice.
let sendShortcutStorageFailed = false;

export function sendShortcut(): SendShortcut {
  if (sendShortcutStorageFailed) return transientSendShortcut;
  try {
    return localStorage.getItem(SEND_SHORTCUT_KEY) === "modifier_enter" ? "modifier_enter" : "enter";
  } catch {
    return transientSendShortcut;
  }
}

export function setSendShortcut(next: SendShortcut): void {
  transientSendShortcut = next;
  try { localStorage.setItem(SEND_SHORTCUT_KEY, next); sendShortcutStorageFailed = false; } catch { sendShortcutStorageFailed = true; }
  sendShortcutListeners.forEach((fn) => fn());
}

function subscribeSendShortcut(fn: () => void): () => void {
  sendShortcutListeners.add(fn);
  const storage = (event: StorageEvent) => {
    if (event.key === SEND_SHORTCUT_KEY || event.key === null) fn();
  };
  window.addEventListener("storage", storage);
  return () => { sendShortcutListeners.delete(fn); window.removeEventListener("storage", storage); };
}

export function useSendShortcut(): SendShortcut {
  return useSyncExternalStore(subscribeSendShortcut, sendShortcut, sendShortcut);
}

// The sidebar remains the default session switcher. Tabs are a local display
// choice and do not change which sessions stay open in the kernel.
const SESSION_TABS_KEY = "rx-show-session-tabs";
let sessionTabsVisible = false;
const sessionTabListeners = new Set<() => void>();

export function showsSessionTabs(): boolean {
  try {
    return localStorage.getItem(SESSION_TABS_KEY) === "on";
  } catch {
    return sessionTabsVisible;
  }
}

export function setShowsSessionTabs(on: boolean): void {
  sessionTabsVisible = on;
  try {
    localStorage.setItem(SESSION_TABS_KEY, on ? "on" : "off");
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  sessionTabListeners.forEach((fn) => fn());
}

export function onShowsSessionTabsChange(fn: () => void): () => void {
  sessionTabListeners.add(fn);
  return () => {
    sessionTabListeners.delete(fn);
  };
}

// How many panes may hold a live event stream at once. Unset is automatic: the
// most the browser's connection limit leaves room for. A browser opens six
// connections to one origin and the session list holds one, so the number is held
// between one and four; anything else reads as automatic.
const LIVE_STREAM_BUDGET_KEY = "rx-live-stream-budget";
export const MAX_LIVE_STREAM_BUDGET = 4;
const liveStreamBudgetListeners = new Set<() => void>();

/** The chosen number, or null for automatic. */
export function liveStreamBudget(): number | null {
  try {
    const raw = localStorage.getItem(LIVE_STREAM_BUDGET_KEY);
    if (raw === null || !/^[1-9][0-9]*$/.test(raw)) return null;
    const n = Number(raw);
    return n >= 1 && n <= MAX_LIVE_STREAM_BUDGET ? n : null;
  } catch {
    return null;
  }
}

export function setLiveStreamBudget(n: number | null): void {
  try {
    if (n !== null && Number.isSafeInteger(n) && n >= 1 && n <= MAX_LIVE_STREAM_BUDGET) localStorage.setItem(LIVE_STREAM_BUDGET_KEY, String(n));
    else localStorage.removeItem(LIVE_STREAM_BUDGET_KEY);
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  liveStreamBudgetListeners.forEach((fn) => fn());
}

export function onLiveStreamBudgetChange(fn: () => void): () => void {
  liveStreamBudgetListeners.add(fn);
  const storage = (event: StorageEvent) => {
    if (event.key === LIVE_STREAM_BUDGET_KEY || event.key === null) fn();
  };
  window.addEventListener("storage", storage);
  return () => {
    liveStreamBudgetListeners.delete(fn);
    window.removeEventListener("storage", storage);
  };
}
