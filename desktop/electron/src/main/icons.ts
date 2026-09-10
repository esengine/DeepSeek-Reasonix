import { existsSync } from "node:fs";
import { join, resolve } from "node:path";

export interface IconLookup {
  platform: NodeJS.Platform;
  appPath: string;
  resourcesPath: string;
  packaged: boolean;
}

export interface IconCandidates {
  tray: string[];
  window: string[];
}

export function iconCandidates(input: IconLookup): IconCandidates {
  const build = input.packaged ? join(input.resourcesPath, "icons") : resolve(input.appPath, "..", "build");
  const hicolor = (size: string) => join(build, "linux", "icons", "hicolor", size, "apps", "reasonix-desktop.png");
  const appicon = join(build, "appicon.png");
  return {
    tray: input.platform === "darwin" ? [appicon, hicolor("32x32")] : [hicolor("32x32"), appicon],
    window: [hicolor("256x256"), appicon],
  };
}

export function firstExisting(paths: string[]): string | null {
  return paths.find((path) => existsSync(path)) ?? null;
}
