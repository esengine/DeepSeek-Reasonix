// Run: tsx src/__tests__/mcp-app-protocol.test.ts
import { readFileSync } from "node:fs";
import {
  normalizeMCPAppResult,
  parseMCPAppArguments,
  parseMCPAppCallResult,
  validatedMCPAppLinkOrigin,
} from "../lib/mcpAppProtocol";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const args = parseMCPAppArguments(`{"city":"Singapore","days":3}`);
ok(args.city === "Singapore" && args.days === 3, "complete tool arguments are parsed");
ok(Object.keys(parseMCPAppArguments("not-json")).length === 0, "invalid arguments degrade to an empty object");
ok(Object.keys(parseMCPAppArguments("[1,2]")).length === 0, "non-object arguments are refused");

const richResult = normalizeMCPAppResult({
  server: "weather",
  tool: "forecast",
  generation: 4,
  rawResult: {
    content: [{ type: "text", text: "rain" }],
    structuredContent: { chance: 80 },
    isError: false,
    _meta: { local: true },
  },
  structured: { chance: 10 },
}, "fallback");
ok(Array.isArray(richResult.content) && richResult.content[0]?.type === "text", "raw MCP result content is preserved");
ok((richResult.structuredContent as { chance?: number }).chance === 80, "raw structuredContent wins over duplicate presentation data");
ok(richResult.isError === false && richResult._meta !== undefined, "standard result fields are preserved");

const fallbackResult = normalizeMCPAppResult({
  server: "weather",
  tool: "forecast",
  generation: 4,
  structured: { chance: 25 },
}, "fallback text");
ok(fallbackResult.content[0]?.type === "text" && fallbackResult.content[0]?.text === "fallback text", "text fallback becomes a CallToolResult content block");
ok((fallbackResult.structuredContent as { chance?: number }).chance === 25, "bounded structured fallback is delivered");

const nestedCallResult = parseMCPAppCallResult(JSON.stringify({
  content: [{
    type: "resource",
    resource: { uri: "https://example.test/result", mimeType: "application/json", text: "{}", _meta: { etag: "v1" } },
    _meta: { audience: "app" },
  }],
  structuredContent: { ok: false, details: [1, 2, 3] },
  isError: true,
  _meta: { trace: "nested" },
}));
ok(nestedCallResult.isError === true, "App-initiated call preserves isError");
ok((nestedCallResult.structuredContent as { details?: number[] }).details?.length === 3, "App-initiated call preserves structuredContent");
const nestedResource = nestedCallResult.content[0] as unknown as { resource?: { _meta?: { etag?: string } }; _meta?: { audience?: string } };
ok(nestedResource.resource?._meta?.etag === "v1" && nestedResource._meta?.audience === "app", "App-initiated call preserves embedded resource metadata");
ok((nestedCallResult._meta as { trace?: string }).trace === "nested", "App-initiated call preserves result metadata");
let invalidNestedResultRejected = false;
try {
  parseMCPAppCallResult(`{"structuredContent":{}}`);
} catch {
  invalidNestedResultRejected = true;
}
ok(invalidNestedResultRejected, "invalid App-initiated CallToolResult is rejected");

ok(validatedMCPAppLinkOrigin("https://docs.example.test/path") === "https://docs.example.test", "https link origin is normalized");
ok(validatedMCPAppLinkOrigin("http://localhost:8080/path") === "http://localhost:8080", "http loopback-style origin is allowed");
for (const unsafe of [
  "javascript:alert(1)",
  "file:///tmp/secret",
  "https://user:pass@example.test/private",
  "//example.test/no-scheme",
]) {
  ok(validatedMCPAppLinkOrigin(unsafe) === null, `unsafe link is refused: ${unsafe}`);
}

const cardSource = readFileSync(new URL("../components/MCPAppCard.tsx", import.meta.url), "utf8");
const inputAt = cardSource.indexOf("bridge.sendToolInput");
const resultAt = cardSource.indexOf("bridge.sendToolResult");
ok(inputAt >= 0 && resultAt > inputAt, "AppBridge sends complete input before the tool result");
ok(cardSource.includes("bridge.teardownResource({})"), "AppBridge teardown is attempted before unmount cleanup");
ok(cardSource.includes("MCPAppCallToolForTab") && cardSource.includes("MCPOpenAppLinkForTab"), "privileged App callbacks use tab-bound host APIs");

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
