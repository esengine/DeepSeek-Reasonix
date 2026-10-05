// Per-machine display choices. They live in localStorage rather than in the
// kernel's settings because they answer "what does this screen show me",
// which is not a fact about the session and does not travel with it.

// On unless this machine turned it off. A turn that changed files and verified
// none of them ends on the one card that says so, and the kernel already
// decides whether there is anything to say.
const RECEIPT_KEY = "rx-turn-receipt";

export function showsReceipt(): boolean {
  try {
    return localStorage.getItem(RECEIPT_KEY) !== "off";
  } catch {
    return true;
  }
}

export function setShowsReceipt(on: boolean): void {
  try {
    localStorage.setItem(RECEIPT_KEY, on ? "on" : "off");
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

// How each foldable part of the transcript starts. "live" opens while the part
// is still being written and folds once it is done; "failed" opens only a step
// that failed; "changed" opens only what wrote a file. A block the reader opened
// or closed keeps that choice.
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

function readFolds(): FoldModes {
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
  return out;
}

let folds: FoldModes | null = null;
const foldListeners = new Set<() => void>();

export function foldModes(): FoldModes {
  folds ??= readFolds();
  return folds;
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
  folds = next;
  try {
    localStorage.setItem(FOLD_KEY, JSON.stringify(next));
  } catch {
    /* the choice holds for this window and is forgotten on the next */
  }
  foldListeners.forEach((fn) => fn());
}

// Service order is a display choice, independent of the provider configuration.
export const PROVIDER_ORDER_KEY = "rx-provider-order";
let providerOrderRaw: string | null | undefined;
let providerOrderValue: readonly string[] = [];
const providerOrderListeners = new Set<() => void>();

export function normalizeProviderOrder(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((item): item is string => typeof item === "string" && item.trim() !== "").map((item) => item.trim()))];
}

export function readProviderOrder(): readonly string[] {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(PROVIDER_ORDER_KEY);
  } catch {
    /* storage is unavailable; keep the default order */
  }
  if (raw !== providerOrderRaw) {
    providerOrderRaw = raw;
    try {
      providerOrderValue = normalizeProviderOrder(JSON.parse(raw ?? "[]"));
    } catch {
      providerOrderValue = [];
    }
  }
  return providerOrderValue;
}

export function writeProviderOrder(order: readonly string[]): boolean {
  const next = normalizeProviderOrder(order);
  const raw = JSON.stringify(next);
  try {
    localStorage.setItem(PROVIDER_ORDER_KEY, raw);
  } catch {
    return false;
  }
  if (raw !== providerOrderRaw) {
    providerOrderRaw = raw;
    providerOrderValue = next;
    providerOrderListeners.forEach((fn) => fn());
  }
  return true;
}

function refreshProviderOrder(event: StorageEvent): void {
  if (event.key !== PROVIDER_ORDER_KEY && event.key !== null) return;
  const before = providerOrderRaw;
  providerOrderRaw = undefined;
  readProviderOrder();
  if (providerOrderRaw !== before) providerOrderListeners.forEach((fn) => fn());
}

export function onProviderOrderChange(fn: () => void): () => void {
  providerOrderListeners.add(fn);
  if (providerOrderListeners.size === 1) window.addEventListener("storage", refreshProviderOrder);
  return () => {
    providerOrderListeners.delete(fn);
    if (providerOrderListeners.size === 0) window.removeEventListener("storage", refreshProviderOrder);
  };
}

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
