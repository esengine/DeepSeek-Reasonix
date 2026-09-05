type LifecycleProbeSnapshot = {
  committedRenders: number;
  liveRenderTokens: number;
  activeOperations: number;
  activeSubscriptions: number;
};

type AppLifecycleProbeApi = {
  snapshot(): LifecycleProbeSnapshot;
};

declare global {
  interface Window {
    __reasonixAppLifecycle?: AppLifecycleProbeApi;
  }
}

const MAX_RENDER_REFS = 2048;
const renderRefs: WeakRef<object>[] = [];
let committedRenders = 0;
let activeOperations = 0;
let activeSubscriptions = 0;

function enabled(): boolean {
  if (typeof window === "undefined") return false;
  const params = new URLSearchParams(window.location.search);
  return params.get("app-lifecycle-probe") === "1" || params.get("bench") === "1";
}

function publishApi(): void {
  if (!enabled() || window.__reasonixAppLifecycle) return;
  window.__reasonixAppLifecycle = {
    snapshot: () => ({
      committedRenders,
      liveRenderTokens: renderRefs.reduce((count, ref) => count + (ref.deref() ? 1 : 0), 0),
      activeOperations,
      activeSubscriptions,
    }),
  };
}

export function createAppRenderToken(): object | null {
  if (!enabled()) return null;
  publishApi();
  return {};
}

export function commitAppRenderToken(token: object | null): void {
  if (!token) return;
  committedRenders += 1;
  renderRefs.push(new WeakRef(token));
  if (renderRefs.length > MAX_RENDER_REFS) {
    renderRefs.splice(0, renderRefs.length - MAX_RENDER_REFS);
  }
}

export function trackAppOperation(delta: 1 | -1): void {
  if (!enabled()) return;
  activeOperations = Math.max(0, activeOperations + delta);
}

export function trackAppSubscription(delta: 1 | -1): void {
  if (!enabled()) return;
  activeSubscriptions = Math.max(0, activeSubscriptions + delta);
}
