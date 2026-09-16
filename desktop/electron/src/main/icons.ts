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
  app: string[];
}

export function iconCandidates(input: IconLookup): IconCandidates {
  const build = input.packaged ? join(input.resourcesPath, "icons") : resolve(input.appPath, "..", "build");
  const hicolor = (size: string) => join(build, "linux", "icons", "hicolor", size, "apps", "reasonix-desktop.png");
  const appicon = join(build, "appicon.png");
  return {
    tray: input.platform === "darwin" ? [appicon, hicolor("32x32")] : [hicolor("32x32"), appicon],
    app: input.platform === "darwin"
      ? [join(build, "darwin", "appicon.png"), appicon]
      : [hicolor("256x256"), appicon],
  };
}

export function firstExisting(paths: string[]): string | null {
  return paths.find((path) => existsSync(path)) ?? null;
}

export function shouldOverrideDockIcon(input: Pick<IconLookup, "platform" | "packaged">): boolean {
  return input.platform === "darwin" && !input.packaged;
}
