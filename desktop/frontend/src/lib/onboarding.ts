export const ONBOARDING_DISMISSED_STORAGE_KEY = "reasonix.onboarding.dismissed.v1";

function onboardingStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

export function onboardingWasDismissed(storage: Storage | null = onboardingStorage()): boolean {
  try {
    return storage?.getItem(ONBOARDING_DISMISSED_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

export function dismissOnboarding(storage: Storage | null = onboardingStorage()): void {
  try {
    storage?.setItem(ONBOARDING_DISMISSED_STORAGE_KEY, "1");
  } catch {
    // A blocked localStorage should not trap the user in the onboarding gate.
  }
}

export function shouldOpenOnboarding(needsProviderSetup: boolean, storage: Storage | null = onboardingStorage()): boolean {
  return needsProviderSetup && !onboardingWasDismissed(storage);
}
