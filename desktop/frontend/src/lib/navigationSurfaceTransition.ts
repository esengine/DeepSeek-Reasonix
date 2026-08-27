export type SurfaceDataOutcome = "ready" | "failed" | "cancelled" | "superseded";

export type SurfaceDataCommit = {
  intent: number;
  outcome: SurfaceDataOutcome;
  tabId?: string;
  surfaceKey?: string;
  error?: string;
};

export type NavigationResult<T> = {
  value: T;
  surfaceReady: Promise<SurfaceDataCommit>;
};

export type NavigationSurfaceState = null | {
  intent: number;
  phase: "source-retained" | "target-masked";
  targetTabId?: string;
  targetSurfaceKey?: string;
};

export type SurfacePaintProgress = { attempts: number; stableFrames: number; geometryKey?: string };
export type SurfacePaintDecision = {
  progress: SurfacePaintProgress;
  outcome?: "ready" | "degraded";
  requestRecovery: boolean;
};

/** Deterministic paint gate shared by Transcript and its fake-clock tests. */
export function advanceSurfacePaintCommit(
  current: SurfacePaintProgress,
  sample: { rendered: boolean; placementReady: boolean; geometryReady: boolean; geometryKey?: string },
): SurfacePaintDecision {
  const attempts = current.attempts + 1;
  const ready = sample.rendered && sample.placementReady && sample.geometryReady && Boolean(sample.geometryKey);
  const stableFrames = ready
    ? (current.geometryKey === sample.geometryKey ? current.stableFrames + 1 : 1)
    : 0;
  const geometryKey = ready ? sample.geometryKey : undefined;
  if (stableFrames >= 2) {
    return { progress: { attempts, stableFrames, geometryKey }, outcome: "ready", requestRecovery: false };
  }
  if (attempts >= 180) {
    return { progress: { attempts, stableFrames, geometryKey }, outcome: "degraded", requestRecovery: false };
  }
  return {
    progress: { attempts, stableFrames, geometryKey },
    requestRecovery: attempts === 60 || attempts === 120,
  };
}

export type NavigationSurfaceIntent = number | null;

export function beginNavigationSurfaceState(intent: number): NavigationSurfaceState {
  return { intent, phase: "source-retained" };
}

/** The Wails/navigation call returned; target data may still be hydrating. */
export function markNavigationTargetMasked(
  current: NavigationSurfaceState,
  intent: number,
  targetTabId?: string,
  targetSurfaceKey?: string,
): NavigationSurfaceState {
  if (current?.intent !== intent) return current;
  return { ...current, phase: "target-masked", targetTabId, targetSurfaceKey };
}

/** Only a target paint terminal may release the opaque surface mask. */
export function settleNavigationSurfaceState(
  current: NavigationSurfaceState,
  completedIntent: number,
): NavigationSurfaceState {
  return current?.intent === completedIntent ? null : current;
}

/** Older completions must never release the latest navigation surface mask. */
export function settleNavigationSurfaceIntent(
  current: NavigationSurfaceIntent,
  completedIntent: number,
): NavigationSurfaceIntent {
  return current === completedIntent ? null : current;
}

type BackendNavigationResultGuard = {
  intent: number;
  targetTabId: string;
  kind: string;
  isIntentCurrent: (intent: number) => boolean;
  reassert: (kind: string, staleTabId: string) => Promise<void>;
};

/**
 * Backend reveal calls activate their returned tab before resolving. A stale
 * frontend result therefore needs an active repair, not just an ignored value.
 */
export async function guardBackendNavigationResult({
  intent,
  targetTabId,
  kind,
  isIntentCurrent,
  reassert,
}: BackendNavigationResultGuard): Promise<boolean> {
  if (isIntentCurrent(intent)) return true;
  await reassert(kind, targetTabId);
  return false;
}
