import type { ExtensionSurface } from "../../port/wire";

let scope = 0;
export const frameKey = (ext: ExtensionSurface) => JSON.stringify([ext.sessionId, ext.generation, ext.pluginId, ext.surfaceId, ext.frame?.frame]);

/** One pane owns this cache; moving a document keeps its state, retiring it does not. */
export class FrameState {
  readonly scope = ++scope;
  private entries = new Map<string, { state?: unknown }>();

  opened(key: string) {
    const previous = this.entries.get(key);
    this.entries.set(key, previous ?? {});
    return previous ? { reason: "remount", ...previous } : { reason: "publish" };
  }

  save(key: string, state: unknown) { this.entries.set(key, { state }); }

  retain(views: ExtensionSurface[]) {
    const keep = new Set(views.filter((v) => v.kind === "frame" && !v.code).map(frameKey));
    for (const key of this.entries.keys()) if (!keep.has(key)) this.entries.delete(key);
  }
}
