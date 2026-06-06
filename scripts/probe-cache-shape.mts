/**
 * Deterministic cache-shape probe.
 *
 * This does not call DeepSeek. It validates local invariants that keep live
 * prefix-cache hit rates high: stable tool-spec ordering, component prefix
 * hashes, and explicit log rewrite tracking.
 *
 * Run: npx tsx scripts/probe-cache-shape.mts
 */

import assert from "node:assert/strict";
import { AppendOnlyLog, ImmutablePrefix } from "../src/memory/runtime.js";
import { ToolRegistry } from "../src/tools.js";
import type { ToolSpec } from "../src/types.js";

function tool(name: string): ToolSpec {
  return {
    type: "function",
    function: {
      name,
      description: `${name} tool`,
      parameters: { type: "object", properties: {} },
    },
  };
}

const registry = new ToolRegistry();
registry.register({ name: "zeta", fn: async () => "z" });
registry.register({ name: "alpha", fn: async () => "a" });
assert.deepEqual(
  registry.specs().map((s) => s.function.name),
  ["alpha", "zeta"],
  "ToolRegistry.specs() must be sorted by tool name",
);

const prefixA = new ImmutablePrefix({ system: "s", toolSpecs: [tool("read"), tool("write")] });
const prefixB = new ImmutablePrefix({ system: "s", toolSpecs: [tool("write"), tool("read")] });
assert.equal(prefixB.fingerprint, prefixA.fingerprint, "reordered tools should hash identically");
assert.equal(
  prefixB.componentHashes.tools,
  prefixA.componentHashes.tools,
  "tool component hash should be stable under reordered input",
);

const log = new AppendOnlyLog();
log.append({ role: "user", content: "hello" });
assert.equal(log.rewriteVersion, 0, "append must not count as a rewrite");
log.compactInPlace([{ role: "assistant", content: "summary" }]);
assert.equal(log.rewriteVersion, 1, "compactInPlace must count as a rewrite");

console.log("PASS: deterministic cache-shape invariants hold");
