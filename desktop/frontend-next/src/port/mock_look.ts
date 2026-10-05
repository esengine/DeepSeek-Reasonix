import { HttpError } from "./port";
import type { Appearance } from "./port";
import { STORAGE as LANG } from "../i18n";

// The appearance half of the fixture. It is held in memory rather than written
// anywhere: there is no config behind a fixture, but the controls still have to
// behave — a slider that snaps back reads as broken, not as unsaved.
//
// Chained into MockPort the way the other faces are, so the class satisfies
// AgentPort in one declaration and each subject keeps its own file.
export class MockLook {
  // The kernel remembers the interface language in its config, so a reload
  // comes back in the language you chose. A fixture that started blank instead
  // reported "follow the machine" on every reload and yanked the window back —
  // the switch looked broken in development and nowhere else.
  private look: Appearance = { language: localStorage.getItem(LANG) ?? undefined };

  async appearance(): Promise<Appearance> {
    return this.look;
  }

  async saveAppearance(look: Appearance): Promise<Appearance> {
    this.look = { ...look, wallpaper: this.look.wallpaper && { ...this.look.wallpaper, ...look.wallpaper } };
    return this.look;
  }

  // The kernel refuses with a code, so the fixture does too — otherwise the
  // path that turns a code into a sentence is never exercised in development.
  private static readonly TYPES = ["image/png", "image/jpeg", "image/webp", "image/avif", "image/gif"];

  // An object URL stands in for the kernel's content-addressed one.
  async uploadWallpaper(blob: Blob): Promise<Appearance> {
    if (!blob.size) throw new HttpError(422, "the image is empty", { code: "wallpaper.empty" });
    if (!MockLook.TYPES.includes(blob.type)) {
      throw new HttpError(422, "unsupported image type", { code: "wallpaper.unsupported_type" });
    }
    this.look = {
      ...this.look,
      wallpaper: { url: URL.createObjectURL(blob), opacity: 0.5, dim: 0.55, focusX: 0.5, focusY: 0.5 },
    };
    return this.look;
  }

  async clearWallpaper(): Promise<void> {
    this.look = { ...this.look, wallpaper: undefined };
  }
}
