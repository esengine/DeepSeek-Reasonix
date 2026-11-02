import { SseBoundary } from "./sse_boundary";
import type { ProviderHealth, ProviderHealthSettings } from "./provider";
import type { Protocol, ProviderCheck, ProviderCheckDraft, ProviderDraft, ProviderEdit, ProviderEntry, ProviderModelCheck, ProviderModelCheckRequest, ProviderProbe, ProviderRemoval, ProviderPreset, ProviderPresetInstall } from "./port";

// Where models come from: the accounts, the protocols their endpoints answer,
// and what probing one found.
export class SseProvider extends SseBoundary {
  providerHealthSettings(): Promise<ProviderHealthSettings> {
    return this.get<ProviderHealthSettings>("/providers/health/settings");
  }
  saveProviderHealthSettings(enabled: boolean): Promise<ProviderHealthSettings> {
    return this.post0<ProviderHealthSettings>("/providers/health/settings", { enabled });
  }
  providerHealth(name: string): Promise<ProviderHealth> {
    return this.post0<ProviderHealth>("/providers/health", { name });
  }
  providers() {
    return this.get<ProviderEntry[]>("/providers");
  }
  protocols() {
    return this.get<Protocol[]>("/providers/protocols");
  }

  providerPresets() {
    return this.get<ProviderPreset[]>("/providers/presets");
  }

  installProviderPreset(req: ProviderPresetInstall) {
    return this.post0<{ names: string[] }>("/providers/presets/install", req);
  }

  // A refused key, a path the endpoint does not serve and an embedding-only
  // gateway are three different fixes. post0 keeps the kernel's coded refusal
  // on HttpError; a bare Error would flatten it back to one string.
  probeProvider(baseUrl: string, apiKey: string): Promise<ProviderProbe> {
    return this.post0<ProviderProbe>("/providers/probe", { baseUrl, apiKey });
  }
  checkProvider(name: string, draft?: ProviderCheckDraft): Promise<ProviderCheck> {
    return this.post0<ProviderCheck>("/providers/check", { name, ...draft });
  }
  checkProviderModel(request: ProviderModelCheckRequest): Promise<ProviderModelCheck> {
    return this.post0<ProviderModelCheck>("/providers/check/model", request);
  }
  saveProvider(draft: ProviderDraft) {
    return this.post("/providers", draft);
  }
  renameProvider(names: string[], displayName: string) {
    return this.post("/providers/display-name", { names, displayName });
  }
  editProvider(edit: ProviderEdit) {
    return this.post("/providers/edit", edit);
  }
  // 204 means nothing besides the service itself changed.
  async removeProvider(name: string): Promise<ProviderRemoval> {
    const body = await this.postMaybe<ProviderRemoval>("/providers/remove", { name });
    return body ?? { movedTo: "", moved: [], cleared: [] };
  }
}
