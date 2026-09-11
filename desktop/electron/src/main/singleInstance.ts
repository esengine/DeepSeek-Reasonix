import { mkdirSync, realpathSync } from "node:fs";
import { join } from "node:path";

export interface InstanceHost {
  setPath(name: "userData", path: string): void;
  requestSingleInstanceLock(): boolean;
}

// Electron keys its process singleton by userData at lock acquisition. Resolve
// symlinks after creating the profile so aliases of one home share one lock.
export function claimShellInstance(host: InstanceHost, dataHome: string, dev: boolean): boolean {
  const profile = join(dataHome, "desktop-shell");
  mkdirSync(profile, { recursive: true });
  host.setPath("userData", realpathSync.native(profile));
  return dev || host.requestSingleInstanceLock();
}
