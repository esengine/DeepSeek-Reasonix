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
