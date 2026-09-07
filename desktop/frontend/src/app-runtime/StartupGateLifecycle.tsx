import { useEffect } from "react";
import { app } from "../lib/bridge";
import { shouldOpenOnboarding } from "../lib/onboarding";
import { useOverlayStore } from "../store/overlays";

export async function probeProviderSetupState(): Promise<boolean> {
  const needs = await app.NeedsOnboarding();
  useOverlayStore.getState().setProviderSetupNeeded(needs);
  return needs;
}

/**
 * Startup onboarding gate: probes whether a provider must be configured and
 * whether the first-run guide should open. Renders nothing; App composes it
 * once beside the other lifecycle components.
 */
export function StartupGateLifecycle() {
  const setNeedsOnboarding = useOverlayStore((state) => state.setNeedsOnboarding);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await probeProviderSetupState();
        if (cancelled) return;
        setNeedsOnboarding(shouldOpenOnboarding(needs));
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [setNeedsOnboarding]);
  return null;
}
