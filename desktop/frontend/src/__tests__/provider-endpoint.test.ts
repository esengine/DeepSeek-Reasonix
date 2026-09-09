import {
  providerBaseURLForSave,
  providerRequestURLForFormatChange,
  providerBaseURLFromRequestURL,
  providerEndpointMismatchDetail,
  providerRequestURLForCatalogFormatChange,
  providerRequestURLFromConfig,
} from "../lib/providerEndpoint";
let failed = 0;
function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    return;
  }
  process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
  failed += 1;
}
console.log("\nprovider endpoint");

eq(providerRequestURLFromConfig("openai", "https://proxy.example.com/v1", ""), "https://proxy.example.com/v1/chat/completions", "legacy OpenAI base URLs expose their effective request URL");
eq(providerRequestURLFromConfig("anthropic", "https://proxy.example.com/v1", ""), "https://proxy.example.com/v1/messages", "legacy Anthropic base URLs expose their effective request URL");
eq(providerRequestURLFromConfig("responses", "https://proxy.example.com/v1", ""), "https://proxy.example.com/v1/responses", "legacy Responses base URLs expose their effective request URL");
eq(providerRequestURLFromConfig("openai", "https://proxy.example.com/v1", "", "https://legacy.example.com/chat/completions/"), "https://legacy.example.com/chat/completions", "legacy OpenAI chat URLs preserve historical trailing-slash normalization");
eq(providerRequestURLFromConfig("anthropic", "https://proxy.example.com/v1", "", "https://stale.example.com/chat/completions"), "https://proxy.example.com/v1/messages", "legacy Anthropic chat URLs remain ignored");
eq(providerRequestURLFromConfig("openai", "", "https://proxy.example.com/custom/chat/?token=1", "https://legacy.example.com/chat/completions"), "https://proxy.example.com/custom/chat/?token=1", "explicit request URLs remain unchanged and take priority");
eq(providerBaseURLFromRequestURL("openai", "https://proxy.example.com/v1/chat/completions?token=1"), "https://proxy.example.com/v1", "request URLs derive a query-free base URL for model discovery");

eq(providerBaseURLForSave({ kind: "openai", baseUrl: "https://api.deepseek.com/v1", chatUrl: "https://gateway.example/custom/chat/completions" }, "openai", "https://gateway.example/custom/chat/completions"), "https://api.deepseek.com/v1", "unchanged legacy providers preserve their independent base URL");
eq(providerBaseURLForSave({ kind: "anthropic", baseUrl: "https://models.example/v1/", requestUrl: "https://gateway.example/custom/messages?token=1" }, "anthropic", "https://gateway.example/custom/messages?token=1"), "https://models.example/v1/", "unchanged explicit request URLs preserve their independent base URL exactly");
eq(providerBaseURLForSave({ kind: "openai", baseUrl: "https://models.example/v1", requestUrl: "https://gateway.example/old/chat/completions" }, "openai", "https://gateway.example/new/chat/completions"), "https://gateway.example/new", "changing the request URL derives a new base URL");
eq(providerBaseURLForSave({ kind: "openai", baseUrl: "https://models.example/v1", requestUrl: "https://gateway.example/v1/chat/completions" }, "anthropic", "https://gateway.example/v1/chat/completions"), "https://gateway.example/v1/chat/completions", "changing protocol derives a new base URL under the new protocol");
eq(providerBaseURLForSave(undefined, "responses", "https://gateway.example/v1/responses"), "https://gateway.example/v1", "new providers derive their base URL from the exact request URL");

eq(providerRequestURLForFormatChange("openai", "responses", "https://gateway.example/v1/chat/completions"), "https://gateway.example/v1/responses", "explicit format switch changes a standard suffix");
eq(providerRequestURLForFormatChange("anthropic", "openai", "https://gateway.example/v1/messages"), "https://gateway.example/v1/chat/completions", "Anthropic v1 is preserved when switching to chat");
eq(providerRequestURLForFormatChange("openai", "responses", "https://gateway.example/custom?token=x"), "https://gateway.example/custom?token=x", "custom request paths and query values remain untouched");
eq(providerRequestURLForFormatChange("openai", "responses", "https://gateway.example/v1/chat/completions?version=1"), "https://gateway.example/v1/chat/completions?version=1", "query-bearing exact overrides are never rewritten");

const deepSeekCatalog = {
  brandId: "deepseek", brandLabel: "DeepSeek", region: "global", product: "api", format: "anthropic", baseUrl: "https://api.deepseek.com/anthropic",
  protocols: {
    openai: { baseUrl: "https://api.deepseek.com/v1", source: "fixture", checkedOn: "2026-09-08" },
    responses: { baseUrl: "https://api.deepseek.com", source: "fixture", checkedOn: "2026-09-08" },
    anthropic: { baseUrl: "https://api.deepseek.com/anthropic", source: "fixture", checkedOn: "2026-09-08" },
  },
};
const mimoCatalog = {
  ...deepSeekCatalog,
  brandId: "mimo",
  protocols: {
    ...deepSeekCatalog.protocols,
    openai: { ...deepSeekCatalog.protocols.openai, baseUrl: "https://api.xiaomimimo.com/v1" },
    responses: { ...deepSeekCatalog.protocols.responses, baseUrl: "https://api.xiaomimimo.com/v1" },
  },
};
eq(providerRequestURLForCatalogFormatChange("anthropic", "openai", "https://api.deepseek.com/anthropic/v1/messages", deepSeekCatalog), "https://api.deepseek.com/v1/chat/completions", "official Anthropic route switches to the registered Chat route");
eq(providerRequestURLForCatalogFormatChange("anthropic", "responses", "https://api.deepseek.com/anthropic/v1/messages", deepSeekCatalog), "https://api.deepseek.com/responses", "official Anthropic route switches to the registered Responses route");
eq(providerRequestURLForCatalogFormatChange("anthropic", "openai", "https://gateway.example/custom/v1/messages", deepSeekCatalog), "https://gateway.example/custom/v1/messages", "custom gateway remains byte-for-byte unchanged");
eq(providerRequestURLForCatalogFormatChange("anthropic", "openai", "https://api.deepseek.com/anthropic/v1/messages?token=x", deepSeekCatalog), "https://api.deepseek.com/anthropic/v1/messages?token=x", "query-bearing official host override stays untouched");
eq(providerEndpointMismatchDetail("openai", "https://api.deepseek.com/anthropic/v1/chat/completions", deepSeekCatalog).mismatch, true, "mixed DeepSeek path is blocked");
eq(providerEndpointMismatchDetail("openai", "https://api.deepseek.com/anthropic/v1/chat/completions", deepSeekCatalog).recommendedUrl, "https://api.deepseek.com/v1/chat/completions", "mixed DeepSeek path recommends the catalog route");
eq(providerEndpointMismatchDetail("openai", "https://api.deepseek.com/chat/completions", deepSeekCatalog).mismatch, false, "working DeepSeek root alias remains valid");
eq(providerEndpointMismatchDetail("openai", "https://api.xiaomimimo.com/v1/chat/completions", mimoCatalog).mismatch, false, "shared Chat and Responses base remains valid");
eq(providerEndpointMismatchDetail("openai", "https://gateway.example/anthropic/v1/chat/completions", deepSeekCatalog).mismatch, false, "custom host is not catalog-rewritten");
eq(providerEndpointMismatchDetail("openai", "https://api.deepseek.com/anthropic/custom/chat/completions", deepSeekCatalog).mismatch, false, "unknown custom path on the official host stays user-owned");

if (failed > 0) process.exit(1);
