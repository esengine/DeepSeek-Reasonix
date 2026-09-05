export type DesktopPlatform = "darwin" | "windows" | "linux";
const MACOS_WORKBENCH_TITLEBAR_HEIGHT = 46;

export function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

export function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

export function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

export function isMacOSWorkbenchSidebarTitlebar(target: HTMLElement | null, clientY: number, platform: DesktopPlatform): boolean {
  if (platform !== "darwin") return false;
  const sidebar = target?.closest(".sidebar--workbench");
  if (!(sidebar instanceof HTMLElement)) return false;
  const offsetY = clientY - sidebar.getBoundingClientRect().top;
  return offsetY >= 0 && offsetY < MACOS_WORKBENCH_TITLEBAR_HEIGHT;
}
