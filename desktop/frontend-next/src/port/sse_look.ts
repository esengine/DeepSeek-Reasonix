import { SseHttp } from "./sse_http";
import { base64Of, guardUpload, tooLarge, UPLOAD_LIMIT_MB } from "./upload";
import type { Appearance, SceneFolder } from "./port";

// The appearance half of the port. Chained the way the fixture's faces are, so
// SsePort satisfies AgentPort in one declaration and each subject keeps its own
// file.
export class SseLook extends SseHttp {
  appearance() {
    return this.get<Appearance>("/appearance");
  }
  saveAppearance(look: Appearance) {
    return this.post0<Appearance>("/appearance", {
      language: look.language ?? "",
      zoom: look.zoom ?? 0,
      readSize: look.readSize ?? 0,
      textSize: look.textSize ?? 0,
      fontUi: look.fontUi ?? "",
      fontMono: look.fontMono ?? "",
      width: look.width ?? "",
      opacity: look.wallpaper?.opacity ?? 0,
      dim: look.wallpaper?.dim ?? 0,
      focusX: look.wallpaper?.focusX ?? 0.5,
      focusY: look.wallpaper?.focusY ?? 0.5,
      muted: look.wallpaper?.muted ?? false,
      volume: look.wallpaper?.volume ?? 1,
    });
  }

  async uploadScene(folder: SceneFolder) {
    guardUpload(folder.scene.size, UPLOAD_LIMIT_MB.scene, tooLarge("wallpaper.scene_too_large", "limit"));
    if (folder.preview) {
      guardUpload(folder.preview.size, UPLOAD_LIMIT_MB.scene, tooLarge("wallpaper.scene_too_large", "limit"));
    }
    return this.post0<Appearance>("/appearance/wallpaper/scene", {
      project: await base64Of(folder.project),
      scene: await base64Of(folder.scene),
      preview: folder.preview ? await base64Of(folder.preview) : "",
    });
  }

  // The picture goes up as the request body, so neither side holds a copy of it
  // beyond what the browser already has: the kernel writes it to disk as it
  // arrives. octet-stream is the one binary type csrfGuard admits (it needs a
  // preflight a cross-site page cannot get), so the real type rides in the query.
  async uploadWallpaper(blob: Blob) {
    const res = await fetch(this.base + "/appearance/wallpaper?mime=" + encodeURIComponent(blob.type), {
      method: "POST",
      headers: { "content-type": "application/octet-stream" },
      credentials: "same-origin",
      body: blob,
    });
    if (!res.ok) await SseHttp.fail("/appearance/wallpaper", res);
    return (await res.json()) as Appearance;
  }
  clearWallpaper() {
    return this.del("/appearance/wallpaper");
  }
}
