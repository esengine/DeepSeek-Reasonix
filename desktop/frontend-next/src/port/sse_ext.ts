import { SseLook } from "./sse_look";
import type { MarketDetail, MarketList, MarketOwnRequest, MarketPackage, MarketPlan, MarketPublished, MarketQuery, MarketRequest, MarketSubmission, MarketVote, PluginExport, PluginInstallRequest, PluginPackage, PluginPlan, ScopeLayer, SkillCatalog, SkillUpdatePlan } from "./port";
import { rootQuery } from "./sse_http";
import { download } from "./download";
import { host } from "./host";
import { readSideAnswer, type SideProgress } from "./side";

function marketPath(slug: string) {
  const [handle = "", name = ""] = slug.split("/", 2);
  return "/market/packages/" + encodeURIComponent(handle) + "/" + encodeURIComponent(name);
}

// Skills and plugin packages: what a project brought with it, and the two acts
// that change that — installing one and switching one off.
export class SseExtensions extends SseLook {
  skills(root?: string) {
    return this.get<SkillCatalog>("/skills" + rootQuery(root));
  }
  reloadExtensions() {
    return this.post("/extensions/reload");
  }
  async extensionSurfaces() {
    const replay = await this.get<import("./surface_replay").ExtensionSurfaceReplay>("/extensions/surfaces");
    return { ...replay, surfaces: replay.surfaces ?? [] };
  }
  async invokeExtensionAction(name: string, args?: Record<string, string>) {
    const out = await this.post0<{ message?: string }>("/extensions/action", args ? { name, args } : { name });
    return out.message ?? "";
  }
  extensionFrameUrl(pluginId: string, frameId: string, generation: number) {
    return `${this.base}/extensions/frames/${encodeURIComponent(pluginId)}/${encodeURIComponent(frameId)}?generation=${generation}`;
  }
  submitExtensionForm(pluginId: string, surfaceId: string, values: Record<string, unknown>) {
    return this.post("/extensions/submit", { pluginId, surfaceId, values });
  }
  setSkillEnabled(name: string, enabled: boolean, scope: ScopeLayer = "project", root?: string) {
    return this.post("/skills/enabled", { name, enabled, scope, root });
  }
  planSkillUpdate(name: string) {
    return this.post0<SkillUpdatePlan>("/skills/" + encodeURIComponent(name) + "/update/plan", {});
  }
  applySkillUpdate(name: string, planId: string, overwriteLocal: boolean) {
    return this.post("/skills/" + encodeURIComponent(name) + "/update/apply", { planId, overwriteLocal });
  }
  removeSkill(name: string, scope: string, path: string, root?: string) {
    return this.post("/skills/remove", { name, scope, path, root });
  }
  clearSkillOverride(name: string, root?: string) {
    return this.post("/skills/enabled", { name, clear: true, scope: "project", root });
  }
  plugins() {
    return this.get<PluginPackage[]>("/plugins");
  }
  planPlugin(req: PluginInstallRequest) {
    return this.post0<PluginPlan>("/plugins/plan", req);
  }
  installPlugin(req: PluginInstallRequest) {
    return this.post0<PluginPlan>("/plugins/install", req);
  }
  setPluginEnabled(name: string, enabled: boolean) {
    return this.post0<{ reloadError?: string }>("/plugins/enabled", { name, enabled });
  }
  removePlugin(name: string): Promise<PluginPlan> {
    return this.del0<PluginPlan>("/plugins/" + encodeURIComponent(name));
  }

  marketList(q: MarketQuery) {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(q)) {
      if (v === undefined || v === "" || v === false) continue;
      params.set(k, v === true ? "1" : String(v));
    }
    const query = params.toString();
    return this.get<MarketList>("/market/packages" + (query ? "?" + query : ""));
  }
  marketDetail(slug: string, opts?: { refresh?: boolean }) {
    return this.get<MarketDetail>(marketPath(slug) + (opts?.refresh ? "?refresh=1" : ""));
  }
  marketMyVote(slug: string) {
    return this.get<MarketVote>(marketPath(slug) + "/vote");
  }
  voteMarket(slug: string, value: -1 | 0 | 1) {
    return this.post0<MarketVote>(marketPath(slug) + "/vote", { value });
  }
  planMarket(req: MarketRequest) {
    return this.post0<MarketPlan>("/market/plan", req);
  }
  installMarket(req: MarketRequest) {
    return this.post0<MarketPlan>("/market/install", req);
  }
  publishMarket(sub: MarketSubmission) {
    return this.post0<MarketPublished>("/market/publish", sub);
  }
  async myMarket() {
    return (await this.get<{ packages: MarketPackage[] }>("/market/mine")).packages;
  }
  planOwnMarket(req: MarketOwnRequest) {
    return this.post0<MarketPlan>("/market/mine/plan", req);
  }
  installOwnMarket(req: MarketOwnRequest) {
    return this.post0<MarketPlan>("/market/mine/install", req);
  }
  async submitMarket(slug: string) {
    const [handle = "", name = ""] = slug.split("/", 2);
    const path = "/market/mine/" + encodeURIComponent(handle) + "/" + encodeURIComponent(name) + "/submit";
    return (await this.post0<{ package: MarketPackage }>(path, {})).package;
  }

  // The header is read before the body because the body is bytes and has
  // nowhere to say what was stripped out of it.
  async exportPlugin(name: string): Promise<PluginExport> {
    const res = await fetch(this.base + "/plugins/" + encodeURIComponent(name) + "/export", {
      credentials: "same-origin",
    });
    if (!res.ok) await SseExtensions.fail(`/plugins/${encodeURIComponent(name)}/export`, res);
    const required = (res.headers.get("X-Reasonix-Required-Env") ?? "").split(",").filter(Boolean);
    const blob = await res.blob();
    // A shell with a save dialog puts the archive where it is asked to; the
    // anchor below is the browser's own way and the only one a tab has.
    const saved = await host().saveBytes(`${name}.zip`, new Uint8Array(await blob.arrayBuffer()));
    if (saved !== null) return { required, savedTo: saved || undefined };
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name}.zip`;
    a.click();
    URL.revokeObjectURL(url);
    return { required };
  }
  async saveText(name: string, content: string): Promise<string | null> {
    const saved = await host().saveText(name, content);
    // null is a shell with no save surface at all; "" is the dialog dismissed,
    // and only the first of those is a reason to fall back to the browser's.
    if (saved !== null) return saved || null;
    download(name, content);
    return null;
  }
  async openSide() {
    return (await this.post0<{ id: string }>("/side", {})).id;
  }
  async askSide(id: string, question: string, quotes: string[], onProgress?: (progress: SideProgress) => void) {
    const path = "/side/" + encodeURIComponent(id) + "/ask";
    const res = await fetch(this.base + path, {
      method: "POST",
      headers: { "content-type": "application/json", accept: "text/event-stream" },
      credentials: "same-origin",
      body: JSON.stringify({ question, quotes }),
    });
    if (!res.ok) await SseExtensions.fail(path, res);
    return readSideAnswer(res, onProgress);
  }
  async closeSide(id: string) {
    await this.del("/side/" + encodeURIComponent(id));
  }
}
