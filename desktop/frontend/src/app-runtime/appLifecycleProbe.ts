export type LifecycleProbeSnapshot = {
  committedRenders: number;
  liveRenderTokens: number;
  liveRenderTokenIds: number[];
  activeOperations: number;
  activeSubscriptions: number;
  invariantViolations: number;
  overflow: boolean;
};

type AppLifecycleProbeApi = { snapshot(): LifecycleProbeSnapshot };

declare global {
  interface Window { __reasonixAppLifecycle?: AppLifecycleProbeApi }
}

// Qualification is finite. Overflow invalidates the evidence; never evict live refs.
const MAX_RENDER_REFS = 65_536;
const renderRefs = new Map<number, WeakRef<object>>();
const renderIds = new WeakMap<object, number>();
let committedRenders = 0;
let activeOperations = 0;
let activeSubscriptions = 0;
let invariantViolations = 0;
let overflow = false;

function enabled(): boolean {
  if (typeof window === "undefined") return false;
  const params = new URLSearchParams(window.location.search);
  return params.get("app-lifecycle-probe") === "1" || params.get("bench") === "1";
}

function liveIds(): number[] {
  const ids: number[] = [];
  for (const [id, ref] of renderRefs) {
    if (ref.deref()) ids.push(id);
    else renderRefs.delete(id);
  }
  return ids;
}

function publishApi(): void {
  if (!enabled() || window.__reasonixAppLifecycle) return;
  window.__reasonixAppLifecycle = {
    snapshot: () => {
      const liveRenderTokenIds = liveIds();
      return {
        committedRenders, liveRenderTokens: liveRenderTokenIds.length, liveRenderTokenIds,
        activeOperations, activeSubscriptions, invariantViolations, overflow,
      };
    },
  };
}

export function createAppRenderToken(): object | null {
  if (!enabled()) return null;
  publishApi();
  return {};
}

export function commitAppRenderToken(token: object | null): void {
  if (!token || renderIds.has(token)) return;
  const id = ++committedRenders;
  renderIds.set(token, id);
  if (renderRefs.size >= MAX_RENDER_REFS) liveIds();
  if (renderRefs.size >= MAX_RENDER_REFS) {
    overflow = true;
    return;
  }
  renderRefs.set(id, new WeakRef(token));
}

export function trackAppOperation(delta: 1 | -1): void {
  if (!enabled()) return;
  activeOperations += delta;
  if (activeOperations < 0) invariantViolations += 1;
}

export function trackAppSubscription(delta: 1 | -1): void {
  if (!enabled()) return;
  activeSubscriptions += delta;
  if (activeSubscriptions < 0) invariantViolations += 1;
}
