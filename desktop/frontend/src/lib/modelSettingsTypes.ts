import type { ProviderView, ProviderModelCatalogUpdate } from "./types";

export type ModelSettingsChange = { requestId: string; expectedFingerprint: string } & (
  | { kind: "preference"; field: "default" | "planner" | "vision" | "search" | "subagent" | "subagent_effort"; ref: string }
  | { kind: "preference"; field: "depth" | "concurrency" | "writers"; number: number }
  | { kind: "preference"; field: "profile_model" | "profile_effort"; name: string; ref: string }
  | { kind: "provider_save"; provider: ProviderView; key?: string }
  | { kind: "credential"; key: string } & ({ name: string; names?: never } | { names: string[]; name?: never })
  | { kind: "web_search_capability"; names: string[]; enabled: boolean }
  | { kind: "connection_add"; presetId?: string; name?: string; key: string; baseURL?: string; protocol?: string }
  | { kind: "official_add"; name: string; key: string }
  | { kind: "preset_add"; presetId: string; key: string }
  | { kind: "preset_reset"; presetId: string }
  | { kind: "protocol_upgrade"; name: string }
  | { kind: "catalogs"; catalogs: ProviderModelCatalogUpdate[] }
  | { kind: "provider_remove" | "access_remove"; names: string[] }
  | { kind: "rename"; names: string[]; ref: string }
);

export interface ModelSettingsResult {
  requestId: string;
  persisted: boolean;
  revision: string;
  application: "not_required" | "pending" | "applied" | "failed";
  targets: { tabId: string; title?: string; application: string; appliedRevision: string; desiredRevision: string }[];
  issues: { code: string; message: string }[];
  appliedCatalogs: string[];
}

