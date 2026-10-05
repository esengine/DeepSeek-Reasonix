import { MockNetwork } from "./mock_network";
import type { ThemeImport, ThemePack } from "./port";

// Theme packs, and where an extension's view was told to sit.
export class MockTheme extends MockNetwork {
  // Mutable: the admin switches are the whole point of the extensions page, and
  // a fixture that answers the same list either way cannot show them working.
  private activeTheme = "";

  private slots: Record<string, string> = {};

  // Two packs so the picker has something to switch between; the fixture is
  // where the mapping gets exercised without a Go process.
  async themes(): Promise<ThemePack[]> {
    return [
      {
        id: "dusk", name: "Dusk", author: "fixture", active: this.activeTheme === "dusk",
        tokens: {
          light: { bg: "#F6F3EE", bgSoft: "#FBF9F5", panel: "#FFFFFF", border: "#DDD5C8", fg: "#1B1814", fgDim: "#5F564A", accent: "#8A5A2B" },
          dark: { bg: "#0F0D0B", bgSoft: "#141110", panel: "#1B1715", border: "#332C26", fg: "#EFE9E1", fgDim: "#9A8F82", accent: "#D89B5A" },
        },
      },
      {
        id: "tide", name: "Tide", author: "fixture", active: this.activeTheme === "tide",
        // One of the two carries a sky, because a live backdrop is a state the
        // picker has to be able to reach: a fixture where no pack has one
        // leaves the whole layer undesignable outside a real kernel.
        sky: { ray: "rgba(255,216,142,.55)", cloud: "255,240,212", cloudLit: "242,206,140", rayAlpha: 0.85, cloudAlpha: 0.4 },
        tokens: {
          light: { bg: "#F2F6F8", bgSoft: "#F9FCFD", panel: "#FFFFFF", border: "#CBD9E0", fg: "#12191C", fgDim: "#4E5D65", accent: "#0E6E82" },
          dark: { bg: "#080D10", bgSoft: "#0C1316", panel: "#131C21", border: "#23333A", fg: "#E4EEF2", fgDim: "#8298A2", accent: "#4FB6CE" },
        },
      },
    ];
  }

  async activateTheme(id: string) {
    this.activeTheme = id;
  }

  async importTheme(files: File[]): Promise<ThemeImport> {
    const name = files[0]?.name.replace(/\.(zip|json)$/i, "") || "imported";
    return { pack: { id: name, name, tokens: { light: { bg: "#FFFFFF" }, dark: { bg: "#000000" } } } };
  }

  async openThemeFolder() {
    return "~/.reasonix/themes";
  }

  // No sidecar runs behind the fixture, so an invocation answers the way a
  // connected extension would rather than pretending to have done work.

  async surfaceSlots() {
    return { ...this.slots };
  }

  async assignSurface(surface: string, slot: string) {
    if (slot) this.slots[surface] = slot;
    else delete this.slots[surface];
  }
}
