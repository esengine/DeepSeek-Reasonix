/** Pure heap-limit decision logic — see heap-limit-launch.ts for the side-effect runner. */

/** Ceiling we'll target. Above this, marginal gains shrink and process startup begins to feel heavy. */
export const TARGET_HEAP_MB_CEILING = 4096;
/** Floor — don't bother re-exec'ing for anything below this. Node's stock 2 GiB default lives here. */
export const TARGET_HEAP_MB_FLOOR = 2048;
/** Slack against the current limit — V8 reports 2090ish for a nominal 2 GiB cap. Treat anything within 64 MiB of target as already-good. */
export const HEAP_HEADROOM_MB = 64;

/** Set on the spawned child so we don't re-exec recursively if Node ignores our flag. */
export const RX_HEAP_REEXEC_ENV = "REASONIX_HEAP_REEXEC";

export interface HeapCheckInputs {
  currentLimitMb: number;
  totalMemMb: number;
  nodeOptions: string;
  execArgv: readonly string[];
  alreadyReexec: boolean;
}

/** Returns the heap target in MiB, or null when no raise is warranted. */
export function decideHeapTargetMb(inputs: HeapCheckInputs): number | null {
  if (inputs.alreadyReexec) return null;
  if (/--max[-_]old[-_]space[-_]size/.test(inputs.nodeOptions)) return null;
  if (inputs.execArgv.some((a) => /max[-_]old[-_]space[-_]size/.test(a))) return null;
  const halfSystem = Math.floor(inputs.totalMemMb / 2);
  const target = Math.min(TARGET_HEAP_MB_CEILING, Math.max(TARGET_HEAP_MB_FLOOR, halfSystem));
  if (inputs.currentLimitMb >= target - HEAP_HEADROOM_MB) return null;
  return target;
}
