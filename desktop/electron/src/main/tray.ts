import { Menu, nativeImage, Tray } from "electron";
import type { TrayLabels } from "./hostCalls.js";
import { errorText, type Logger } from "./log.js";

export interface TrayDeps {
  platform: NodeJS.Platform;
  iconPath: string | null;
  onOpen(): void;
  onQuit(): void;
  log: Logger;
}

export class TrayHost {
  private tray: Tray | null = null;

  constructor(private readonly deps: TrayDeps) {}

  ensure(labels: TrayLabels): { ready: boolean; reason: string } {
    const { deps } = this;
    if (!deps.iconPath) return { ready: false, reason: "tray icon asset missing" };
    try {
      if (!this.tray) {
        let image = nativeImage.createFromPath(deps.iconPath);
        if (image.isEmpty()) return { ready: false, reason: `tray icon could not be decoded: ${deps.iconPath}` };
        if (deps.platform === "darwin") {
          image = image.resize({ width: 18, height: 18 });
          image.setTemplateImage(true);
        } else if (deps.platform === "win32") {
          image = image.resize({ width: 16, height: 16 });
        }
        const tray = new Tray(image);
        tray.on("click", () => deps.onOpen());
        this.tray = tray;
      }
      this.tray.setToolTip(labels.tooltip || "Reasonix");
      this.tray.setContextMenu(Menu.buildFromTemplate([
        { label: labels.openTitle, toolTip: labels.openTooltip, click: () => deps.onOpen() },
        { label: labels.quitTitle, toolTip: labels.quitTooltip, click: () => deps.onQuit() },
      ]));
      return { ready: true, reason: "" };
    } catch (error) {
      deps.log.warn(`tray unavailable: ${errorText(error)}`);
      this.destroy();
      return { ready: false, reason: errorText(error) };
    }
  }

  destroy(): void {
    try {
      this.tray?.destroy();
    } catch {
      // Already gone.
    }
    this.tray = null;
  }
}
