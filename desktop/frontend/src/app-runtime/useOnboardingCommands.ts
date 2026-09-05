import { dismissOnboarding } from "../lib/onboarding";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useOverlayStore } from "../store/overlays";

export function useOnboardingCommands(providerConfigured: () => void) {
  const completeOnboarding = useCommittedCommand(() => {
    providerConfigured();
    useOverlayStore.getState().setNeedsOnboarding(false);
  });
  const chooseOnboardingProvider = useCommittedCommand(() => {
    const overlays = useOverlayStore.getState();
    overlays.setNeedsOnboarding(false);
    overlays.setSettingsFocus({ target: "model-access" });
    overlays.setSettingsTarget("models");
  });
  const skipOnboarding = useCommittedCommand(() => {
    dismissOnboarding();
    useOverlayStore.getState().setNeedsOnboarding(false);
  });
  return { completeOnboarding, chooseOnboardingProvider, skipOnboarding };
}
