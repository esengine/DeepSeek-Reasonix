import { statSync } from "node:fs";
import { posix, win32 } from "node:path";

export interface ServiceBinaryLookup {
  env: NodeJS.ProcessEnv;
  platform: NodeJS.Platform;
  execPath: string;
  resourcesPath: string;
  isFile?: (path: string) => boolean;
}

export interface ServiceBinaryResolution {
  binary: string;
  probed: string[];
}

const DEB_SHELL_DIR = "/usr/lib/reasonix";

function isRegularFile(path: string): boolean {
  try { return statSync(path).isFile(); } catch { return false; }
}

// Inverse of the Go service's shellPathForExecutable (desktop/shell_bootstrap.go).
// The service normally hands its own path over in REASONIX_DESKTOP_SERVICE; a
// shell started directly (pinned taskbar icon, double-click) must find it.
export function resolveServiceBinary({ env, platform, execPath, resourcesPath, isFile = isRegularFile }: ServiceBinaryLookup): ServiceBinaryResolution {
  const configured = (env.REASONIX_DESKTOP_SERVICE ?? "").trim();
  if (configured !== "") return { binary: configured, probed: [] };
  const path = platform === "win32" ? win32 : posix;
  const name = platform === "win32" ? "reasonix-desktop.exe" : "reasonix-desktop";
  const bundled = path.join(resourcesPath, "service", name);
  const candidates: string[] = [];
  const appDir = path.dirname(execPath);
  if (path.basename(appDir).toLowerCase() === "app") {
    const releaseDir = path.dirname(appDir);
    candidates.push(path.join(releaseDir, name));
    if (platform === "linux" && releaseDir === DEB_SHELL_DIR) candidates.push(path.join("/usr/bin", name));
  }
  candidates.push(bundled);
  return { binary: candidates.find(isFile) ?? bundled, probed: candidates };
}
