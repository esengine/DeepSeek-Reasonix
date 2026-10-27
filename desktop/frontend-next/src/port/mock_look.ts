import { HttpError } from "./port";
import type { Appearance, SceneFolder } from "./port";
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

  // What the kernel announces; the zoom test holds it equal to config.ZoomMin..ZoomMax.
  private static readonly ZOOM = { min: 0.8, max: 1.8, step: 0.05 };

  async appearance(): Promise<Appearance> {
    return { ...this.look, zoomRange: MockLook.ZOOM };
  }

  async saveAppearance(look: Appearance): Promise<Appearance> {
    const { min, max } = MockLook.ZOOM;
    const zoom = look.zoom ? Math.min(Math.max(look.zoom, min), max) : look.zoom;
    this.look = { ...look, zoom, wallpaper: this.look.wallpaper && { ...this.look.wallpaper, ...look.wallpaper } };
    return this.appearance();
  }

  // The kernel refuses with a code, so the fixture does too — otherwise the
  // path that turns a code into a sentence is never exercised in development.
  private static readonly TYPES = ["image/png", "image/jpeg", "image/webp", "image/avif", "image/gif", "video/mp4", "video/webm"];

  // An object URL stands in for the kernel's content-addressed one.
  async uploadWallpaper(blob: Blob): Promise<Appearance> {
    if (!blob.size) throw new HttpError(422, "the image is empty", { code: "wallpaper.empty" });
    if (!MockLook.TYPES.includes(blob.type)) {
      throw new HttpError(422, "unsupported image type", { code: "wallpaper.unsupported_type" });
    }
    this.look = {
      ...this.look,
      wallpaper: { url: URL.createObjectURL(blob), kind: blob.type.startsWith("video/") ? "video" : "image", opacity: 0.5, dim: 0.55, focusX: 0.5, focusY: 0.5 },
    };
    return this.appearance();
  }

  async clearWallpaper(): Promise<void> {
    this.look = { ...this.look, wallpaper: undefined };
  }

  async uploadScene(folder: SceneFolder): Promise<Appearance> {
    if (!folder.project.size || !folder.scene.size) {
      throw new HttpError(422, "the folder is incomplete", { code: "wallpaper.scene_invalid" });
    }
    let title = "";
    try {
      title = (JSON.parse(await folder.project.text()) as { title?: string }).title?.trim() ?? "";
    } catch {
      // A malformed project still leaves a scene the fixture can show.
    }
    this.look = {
      ...this.look,
      wallpaper: {
        url: URL.createObjectURL(folder.preview ?? folder.scene),
        kind: "scene",
        title: title || "Wallpaper Engine scene",
        parallax: 0.02,
        muted: true,
        volume: 1,
        opacity: 0.5,
        dim: 0.55,
        focusX: 0.5,
        focusY: 0.5,
      },
    };
    return this.appearance();
  }
}
