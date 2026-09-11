import { useEffect } from "react";
import { app } from "../lib/bridge";
import { dismissOnboarding, shouldOpenOnboarding } from "../lib/onboarding";
import { useOverlayStore } from "../store/overlays";
import { useAppNavigationStore } from "../store/appNavigation";

export async function probeProviderSetupState(): Promise<boolean> {
  const needs = await app.NeedsOnboarding();
  useOverlayStore.getState().setProviderSetupNeeded(needs);
  return needs;
}

/** Probes setup once; a later navigation intent owns the current page. */
export function StartupGateLifecycle() {
  useEffect(() => {
    let cancelled = false;
    const navigationGeneration = useAppNavigationStore.getState().generation;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (cancelled) return;
        useOverlayStore.getState().setProviderSetupNeeded(needs);
        const navigation = useAppNavigationStore.getState();
        if (shouldOpenOnboarding(needs) && navigation.generation === navigationGeneration) {
          dismissOnboarding();
          navigation.setSettingsFocus({ target: "model-access", onboarding: true });
          navigation.setSettingsTarget("providers");
        }
      } catch {
        // Setup status is advisory; bridge failures must not block startup.
      }
    })();
    return () => { cancelled = true; };
  }, []);
  return null;
}
