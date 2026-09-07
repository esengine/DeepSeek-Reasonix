import { dismissOnboarding } from "../lib/onboarding";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useOverlayStore } from "../store/overlays";
import { useAppNavigationStore } from "../store/appNavigation";

export function useOnboardingCommands(providerConfigured: () => void) {
  const completeOnboarding = useCommittedCommand(() => {
    providerConfigured();
    useOverlayStore.getState().setNeedsOnboarding(false);
  });
  const chooseOnboardingProvider = useCommittedCommand(() => {
    const overlays = useOverlayStore.getState();
    overlays.setNeedsOnboarding(false);
    const navigation = useAppNavigationStore.getState();
    navigation.setSettingsFocus({ target: "model-access" });
    navigation.setSettingsTarget("models");
  });
  const skipOnboarding = useCommittedCommand(() => {
    dismissOnboarding();
    useOverlayStore.getState().setNeedsOnboarding(false);
  });
  return { completeOnboarding, chooseOnboardingProvider, skipOnboarding };
}
