import { SseHttp } from "./sse_http";
import type { Appearance } from "./port";

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
      fontUi: look.fontUi ?? "",
      fontMono: look.fontMono ?? "",
      width: look.width ?? "",
      opacity: look.wallpaper?.opacity ?? 0,
      dim: look.wallpaper?.dim ?? 0,
      focusX: look.wallpaper?.focusX ?? 0.5,
      focusY: look.wallpaper?.focusY ?? 0.5,
    });
  }

  // JSON, not raw bytes: csrfGuard admits nothing else, which is what stops a
  // cross-site form from posting here at all.
  // JSON, not raw bytes: csrfGuard admits nothing else, which is what stops a
  // cross-site form from posting here at all.
  async uploadWallpaper(blob: Blob) {
    const buf = new Uint8Array(await blob.arrayBuffer());
    let bin = "";
    for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
    const res = await fetch(this.base + "/appearance/wallpaper", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ mime: blob.type, data: btoa(bin) }),
    });
    const body = (await res.json().catch(() => ({}))) as Appearance & { error?: string };
    if (!res.ok) throw new Error(body.error || `/appearance/wallpaper: ${res.status}`);
    return body;
  }
  async clearWallpaper() {
    const res = await fetch(this.base + "/appearance/wallpaper", {
      method: "DELETE",
      credentials: "same-origin",
    });
    if (!res.ok) throw new Error(`/appearance/wallpaper: ${res.status}`);
  }
}
