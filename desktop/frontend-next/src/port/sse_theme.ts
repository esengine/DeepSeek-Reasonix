import { SseNetwork } from "./sse_network";
import type { ThemeImport, ThemePack } from "./port";
import { t } from "../i18n";
import { base64Of, guardUpload, UPLOAD_LIMIT_MB } from "./upload";

// The size of the JSON body importTheme will send, which is what the kernel caps:
// each file in padded base64 under its quoted name, or the one archive.
export function themeBodyBytes(files: { name: string; size: number }[]): number {
  const quoted = (name: string) => new TextEncoder().encode(JSON.stringify(name)).length;
  const encoded = (size: number) => Math.ceil(size / 3) * 4 + 2;
  const zip = files.find((f) => f.name.toLowerCase().endsWith(".zip"));
  if (zip) return `{"name":,"zip":}`.length + quoted(zip.name) + encoded(zip.size);
  return `{"files":{}}`.length + files.reduce((n, f, i) => n + quoted(f.name) + 1 + encoded(f.size) + (i ? 1 : 0), 0);
}

// Theme packs, and where an extension's view is allowed to sit. Both are the
// user overruling a default the pack or the extension asked for.
export class SseTheme extends SseNetwork {
  themes() {
    return this.get<ThemePack[]>("/themes");
  }
  activateTheme(id: string) {
    return this.post("/themes", { id });
  }
  removeTheme(id: string) {
    return this.del(`/themes/${encodeURIComponent(id)}`);
  }
  // JSON, not multipart: csrfGuard admits nothing else.
  async importTheme(files: File[]) {
    const zip = files.find((f) => f.name.toLowerCase().endsWith(".zip"));
    if (zip && files.length > 1) throw new Error(t("look.choose_a_single_zip_on"));
    guardUpload(themeBodyBytes(files), UPLOAD_LIMIT_MB.theme, (limit) => new Error(t("look.the_theme_pack_is_too", { limit })));
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
