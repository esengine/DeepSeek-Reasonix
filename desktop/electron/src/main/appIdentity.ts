import { statSync } from "node:fs";
import { win32 } from "node:path";
import type { AppDetailsOptions, BrowserWindow } from "electron";

// Share the launcher's identity without grouping with old or current Studio.
// internal/appidentity/electron_identity_test.go guards the Go/JS contract.

export const APP_USER_MODEL_ID = "io.reasonix.desktop";

// Applied at module load: the ID must be in place before the first window
// exists, and Windows ignores the call on other platforms.
export function applyAppUserModelId(target: Pick<typeof import("electron").app, "setAppUserModelId">, platform: NodeJS.Platform): void {
  if (platform === "win32") target.setAppUserModelId(APP_USER_MODEL_ID);
}

interface WindowCreationEvents {
  on(event: "browser-window-created", listener: (event: unknown, window: Pick<BrowserWindow, "setAppDetails">) => void): unknown;
}

function isRegularFile(path: string): boolean {
  try { return statSync(path).isFile(); } catch { return false; }
}

export function windowsTaskbarDetails(executable: string, isFile = isRegularFile): AppDetailsOptions | undefined {
  if (!win32.isAbsolute(executable)) return undefined;
  const appDir = win32.dirname(executable);
  if (win32.basename(appDir).toLowerCase() !== "app") return undefined;
  const releaseDir = win32.dirname(appDir);
  const versioned = win32.basename(win32.dirname(releaseDir)).toLowerCase() === "versions"
    && /^v[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.-]+)?$/.test(win32.basename(releaseDir));
  const root = versioned ? win32.dirname(win32.dirname(releaseDir)) : releaseDir;
  const launcher = win32.join(root, "reasonix-launcher.exe");
  if (!isFile(launcher)) return undefined;
  return {
    appId: APP_USER_MODEL_ID,
    appIconPath: launcher,
    appIconIndex: 0,
    relaunchCommand: `"${launcher}"`,
    relaunchDisplayName: "Reasonix",
  };
}

// An AppID alone makes Explorer pin the versioned Electron executable. Stamp
// every window before it is shown so new pins also survive version retention.
export function registerTaskbarRelaunch(target: WindowCreationEvents, platform: NodeJS.Platform, executable: string, packaged: boolean, isFile = isRegularFile): void {
  if (platform !== "win32" || !packaged) return;
  const details = windowsTaskbarDetails(executable, isFile);
  if (details) target.on("browser-window-created", (_event, window) => window.setAppDetails(details));
}
