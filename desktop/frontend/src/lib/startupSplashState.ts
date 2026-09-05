const SPLASH_FLAG = "reasonix.splash.shown";

export function shouldShowStartupSplash(): boolean {
  try {
    return window.sessionStorage.getItem(SPLASH_FLAG) !== "1";
  } catch {
    return true;
  }
}

export function markSplashShown(): void {
  try {
    window.sessionStorage.setItem(SPLASH_FLAG, "1");
  } catch {
    // Session storage is optional in restricted hosts.
  }
}
