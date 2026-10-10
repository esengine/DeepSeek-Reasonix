import { SseNetwork } from "./sse_network";
import type { ThemeImport, ThemePack } from "./port";
import { t } from "../i18n";

// Theme packs, and where an extension's view is allowed to sit. Both are the
// user overruling a default the pack or the extension asked for.
export class SseTheme extends SseNetwork {
  themes() {
    return this.get<ThemePack[]>("/themes");
  }
  activateTheme(id: string) {
    return this.post("/themes", { id });
  }
  // JSON, not multipart: csrfGuard admits nothing else.
  async importTheme(files: File[]) {
    const zip = files.find((f) => f.name.toLowerCase().endsWith(".zip"));
    if (zip && files.length > 1) throw new Error(t("请单独选择一个 .zip，或选择不含压缩包的 theme.json 和图片。"));
    if (zip) return this.post0<ThemeImport>("/themes/import", { name: zip.name, zip: await base64Of(zip) });
    const bodies: Record<string, string> = {};
    for (const f of files) bodies[f.name] = await base64Of(f);
    return this.post0<ThemeImport>("/themes/import", { files: bodies });
  }
  async openThemeFolder() {
    return (await this.post0<{ path: string }>("/themes/folder", {})).path;
  }
  // The extension's own message is the result, so this reads the body rather
  // than a status code. A refused action answers 422 with its reason.
  async surfaceSlots() {
    const r = await this.get<{ slots?: Record<string, string> }>("/surfaces");
    return r.slots ?? {};
  }
  assignSurface(surface: string, slot: string) {
    return this.post("/surfaces", { surface, slot });
  }
}

async function base64Of(blob: Blob): Promise<string> {
  const buf = new Uint8Array(await blob.arrayBuffer());
  let bin = "";
  for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
  return btoa(bin);
}
