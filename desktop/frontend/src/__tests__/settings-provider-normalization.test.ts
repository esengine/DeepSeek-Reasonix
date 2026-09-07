import {
  formatProviderExtraBody,
  normalizeProviderView,
  parseProviderExtraBody,
  providerEditorEffectiveKind,
  providerExtraBodyParseError,
} from "../components/SettingsPanel";
import type { ProviderView } from "../lib/types";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1; else failed += 1;
}
function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, actual === expected ? label : `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

console.log("\nsettings provider normalization");
const nullable = normalizeProviderView({ name: null, baseUrl: null } as unknown as ProviderView);
eq(nullable.name, "", "provider snapshots normalize a null name at the settings boundary");
eq(nullable.baseUrl, "", "provider snapshots normalize a null base URL at the settings boundary");
eq(normalizeProviderView({ name: "glm", baseUrl: "https://gateway.example/v1", reasoningProtocol: "glm" } as ProviderView).reasoningProtocol, "glm", "provider snapshots preserve the GLM protocol");
eq(normalizeProviderView({ name: "anthropic", kind: "anthropic", baseUrl: "https://gateway.example", serverWebSearchCapability: true } as ProviderView).serverWebSearchCapability, true, "provider snapshots preserve server web-search capability");
eq(normalizeProviderView({ name: "legacy", kind: "anthropic", baseUrl: "https://gateway.example" } as ProviderView).serverWebSearchCapability, undefined, "older snapshots keep an absent capability distinguishable");
eq(providerEditorEffectiveKind(true, "anthropic", ["anthropic", "openai"]), "anthropic", "new custom providers keep the selected kind");
eq(providerEditorEffectiveKind(false, "anthropic", ["anthropic", "openai"]), "anthropic", "existing providers preserve their stored kind");
eq(formatProviderExtraBody({ top_p: 0.7, enable_thinking: true }), "{\n  \"enable_thinking\": true,\n  \"top_p\": 0.7\n}", "extra body editor formats stable JSON");
eq(JSON.stringify(parseProviderExtraBody('{ "enable_thinking": true, "top_p": 0.7 }')), '{"enable_thinking":true,"top_p":0.7}', "extra body editor parses an object");
let rejected = false;
try { parseProviderExtraBody("[true]"); } catch { rejected = true; }
ok(rejected, "extra body editor rejects non-object JSON");
const t = ((key: string, vars?: Record<string, string | number>) => key === "settings.providerExtraBodyNull" ? `${vars?.path} localized null` : key === "settings.providerExtraBodyError" ? "localized fallback" : key) as any;
eq(providerExtraBodyParseError(new SyntaxError("bad JSON"), t), "localized fallback", "extra body editor localizes syntax errors");
try {
  parseProviderExtraBody('{ "nested": { "value": null } }', t);
  ok(false, "extra body editor rejects null values");
} catch (error) {
  eq(providerExtraBodyParseError(error, t), "extra_body.nested.value localized null", "extra body editor retains the structured validation path");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
