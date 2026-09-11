import type { AppBindings } from "./bridge";
import type { ProviderPresetView, SettingsView } from "./types";
import type { ModelSettingsChange, ModelSettingsResult } from "./modelSettingsTypes";

export interface ModelSettingsBindings {
  Settings(): Promise<SettingsView>;
  ApplyModelSettings(change: ModelSettingsChange): Promise<ModelSettingsResult>;
  GetModelSettingsRequest(requestId: string): Promise<ModelSettingsResult>;
  GetModelSettingsApplication(): Promise<ModelSettingsResult>;
  RetryModelSettingsApplication(tabID: string): Promise<ModelSettingsResult>;
}

export function makeMockModelSettingsBindings(
  settings: SettingsView,
  loadCatalog: () => Promise<void> = async () => {},
  presets: () => ProviderPresetView[] = () => [],
): ModelSettingsBindings {
  const receipts = new Map<string, ModelSettingsResult>();
  const remember = (result: ModelSettingsResult) => {
    receipts.set(result.requestId, result);
    if (receipts.size > 128) receipts.delete(receipts.keys().next().value!);
    return result;
  };
  return {
    async Settings() {
      await loadCatalog();
      for (const preset of presets()) {
        const existing = settings.providerPresets.find(p => p.id === preset.id);
        if (existing) existing.catalog = preset.catalog;
        else settings.providerPresets.push(preset);
      }
      return JSON.parse(JSON.stringify(settings)) as SettingsView;
    },
    async GetModelSettingsApplication(this: AppBindings): Promise<ModelSettingsResult> {
      return {requestId: "", persisted: true, revision: settings.modelSettingsFingerprint!, application: "not_required", targets: [], issues: [], appliedCatalogs: []};
    },
    async RetryModelSettingsApplication(this: AppBindings, _tabID: string): Promise<ModelSettingsResult> {
      return this.GetModelSettingsApplication();
    },
    async ApplyModelSettings(this: AppBindings, change: ModelSettingsChange): Promise<ModelSettingsResult> {
      const receipt = receipts.get(change.requestId);
      if (receipt) return receipt;
      const result = await this.GetModelSettingsApplication();
      result.requestId = change.requestId;
      if (change.expectedFingerprint !== settings.modelSettingsFingerprint) {
        return remember({...result, persisted: false, issues: [{code: "conflict", message: "Model settings changed; reload before saving."}]});
      }
      try {
      switch (change.kind) {
        case "preference":
          switch (change.field) {
            case "default": await this.SetDefaultModel(change.ref); break;
            case "planner": await this.SetPlannerModel(change.ref); break;
            case "vision": await this.SetVisionModel(change.ref); break;
            case "search": await this.SetWebSearchModel(change.ref); break;
            case "subagent": await this.SetSubagentModel(change.ref); break;
            case "subagent_effort": await this.SetSubagentEffort(change.ref); break;
            case "depth": await this.SetMaxSubagentDepth(change.number); break;
            case "concurrency": await this.SetMaxSubagentConcurrency(change.number); break;
            case "writers": await this.SetMaxParallelWriters(change.number); break;
            case "profile_model": await this.SetSubagentProfileModel(change.name, change.ref); break;
            case "profile_effort": await this.SetSubagentProfileEffort(change.name, change.ref); break;
          }
          break;
        case "provider_save":
          if (change.key !== undefined) await this.SaveProviderWithKey(change.provider, change.key);
          else await this.SaveProvider(change.provider);
          break;
        case "credential": for (const name of change.names ?? [change.name]) await this.SetConnectionKey(name, change.key); break;
        case "web_search_capability": await this.SetProviderWebSearch(change.names, change.enabled); break;
        case "connection_add": await this.AddProviderConnectionWithOptions(change.presetId ?? "", change.name ?? "", change.key, change.baseURL ?? "", change.protocol ?? ""); break;
        case "official_add": await this.AddOfficialProviderAccess(change.name, change.key); break;
        case "preset_add": await this.AddProviderPresetAccess(change.presetId, change.key); break;
        case "preset_reset": await this.ResetProviderPresetAccess(change.presetId); break;
        case "protocol_upgrade": await this.UpgradeDeepSeekProviderAccess(change.name); break;
        case "catalogs": result.appliedCatalogs = await this.SaveProviderModelCatalogs(change.catalogs); break;
        case "provider_remove": for (const name of change.names) await this.DeleteProvider(name); break;
        case "access_remove": await this.RemoveProviderAccesses(change.names); break;
        case "rename": await this.RenameProviderConnections(change.names, change.ref); break;
      }
      settings.modelSettingsFingerprint = `mock-model-settings-${crypto.randomUUID()}`;
      return remember({...result, revision: settings.modelSettingsFingerprint});
      } catch {
        return remember({...result, persisted: false, issues: [{code: "validation_failed", message: "Model settings could not be saved. Check the edited fields."}]});
      }
    },
    async GetModelSettingsRequest(this: AppBindings, requestId: string) {
      const receipt = receipts.get(requestId);
      if (receipt) return receipt;
      return {...await this.GetModelSettingsApplication(), requestId, persisted: false, issues: [{code: "unknown_result", message: "The save result could not be confirmed. Review the current settings before saving again."}]};
    },
  };
}
