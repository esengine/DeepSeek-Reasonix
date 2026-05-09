/** Cross-platform URL opener; no-op under CI / when REASONIX_NO_OPEN is set. */

import { spawn } from "node:child_process";
import { platform } from "node:os";

export interface OpenUrlResult {
  opened: boolean;
  reason?: "ci" | "disabled" | "spawn-failed";
}

export function openUrl(url: string): OpenUrlResult {
  if (process.env.CI) return { opened: false, reason: "ci" };
  if (process.env.REASONIX_NO_OPEN) return { opened: false, reason: "disabled" };

  const os = platform();
  let cmd: string;
  let args: string[];
  if (os === "win32") {
    cmd = "cmd";
    args = ["/c", "start", "", url];
  } else if (os === "darwin") {
    cmd = "open";
    args = [url];
  } else {
    cmd = "xdg-open";
    args = [url];
  }

  try {
    const child = spawn(cmd, args, { detached: true, stdio: "ignore" });
    child.unref();
    return { opened: true };
  } catch {
    return { opened: false, reason: "spawn-failed" };
  }
}
