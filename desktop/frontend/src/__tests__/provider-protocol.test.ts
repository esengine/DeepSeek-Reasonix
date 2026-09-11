import assert from "node:assert/strict";
import { providerProtocolLabel, providerEndpointMismatch, providerProtocolChoices } from "../lib/providerProtocol";
assert.equal(providerProtocolLabel("dashscope-responses"), "百炼 Responses (/responses)");
assert.equal(providerProtocolLabel("anthropic"), "Anthropic Messages (/v1/messages)");
assert.equal(providerEndpointMismatch("dashscope-responses", "https://api.deepseek.com/anthropic/v1/messages"), true);
assert.equal(providerEndpointMismatch("anthropic", "https://example.test/v1/messages"), false);
assert.equal(providerEndpointMismatch("openai", "https://example.test/v1/chat/completions?x=1"), false);
assert.equal(providerEndpointMismatch("responses", "https://example.test/v1"), false);
assert.equal(providerEndpointMismatch("responses", "https://example.test/gateway"), false);
assert.equal(providerEndpointMismatch("custom", "https://example.test/v1/messages"), false);
console.log("provider protocol: PASS");

const registered = ["anthropic", "dashscope-responses", "openai", "responses", "vendor-private"];
assert.deepEqual(providerProtocolChoices("openai", undefined, registered), ["openai", "anthropic", "responses"]);
assert.deepEqual(providerProtocolChoices("anthropic", "anthropic", registered), ["anthropic", "openai", "responses"]);
assert.ok(providerProtocolChoices("openai", "dashscope-responses", registered).includes("dashscope-responses"));
assert.ok(providerProtocolChoices("dashscope-responses", "dashscope-responses", registered).includes("dashscope-responses"));
assert.ok(providerProtocolChoices("openai", undefined, ["openai", "dashscope-responses"], true).includes("dashscope-responses"));
console.log("provider protocol choices: PASS");
