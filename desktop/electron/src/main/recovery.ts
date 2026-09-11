import { existsSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";

// Resolve only the stable entry in this shell's own versioned installation.
// Never relaunch a path supplied by a status client or keep an old service path.
export function supersededLauncher(shellPath: string, version: string): string | undefined {
  const versions = dirname(dirname(dirname(shellPath)));
  if (basename(versions).toLowerCase() !== "versions") return undefined;
  const root = dirname(versions);
  try {
    const current = JSON.parse(readFileSync(join(root, "current.json"), "utf8"));
    if (current.schemaVersion !== 1 || typeof current.activeVersion !== "string" || current.activeVersion === version) return undefined;
    if (!/^v[0-9A-Za-z][0-9A-Za-z._+-]*$/.test(current.activeVersion) || current.activeVersion.includes("..") || current.activeDir !== `versions/${current.activeVersion}`) return undefined;
    const launcher = join(root, "reasonix-launcher.exe");
    return existsSync(launcher) ? launcher : undefined;
  } catch { return undefined; }
}
