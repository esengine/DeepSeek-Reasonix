// Presentation and conservative endpoint checks shared by provider settings.
import type { ProviderCatalog } from "./providerCatalogTypes";
import { providerEndpointMismatchDetail } from "./providerEndpoint";

export function providerProtocolLabel(kind: string): string {
  switch (kind.trim().toLowerCase()) {
    case "anthropic": return "Anthropic Messages (/v1/messages)";
    case "openai": return "Chat Completions (/chat/completions)";
    case "responses": return "Responses (/responses)";
    case "dashscope-responses": return "百炼 Responses (/responses)";
    default: return kind;
  }
}

export function providerEndpointMismatch(kind: string, address: string, catalog?: ProviderCatalog): boolean {
  return providerEndpointMismatchDetail(kind, address, catalog).mismatch;
}

// Registered adapters are not all user-facing protocols. Keep saved/custom
// adapter IDs selectable without exposing them on unrelated connections.
export function providerProtocolChoices(current: string, saved: string | undefined, registered: string[], presetScoped = false): string[] {
  const generic = new Set(["openai", "responses", "anthropic"]);
  const choices = [current, saved ?? "", ...registered.filter(kind => presetScoped || generic.has(kind.trim()))];
  return [...new Set(choices.map(kind => kind.trim()).filter(Boolean))];
}
