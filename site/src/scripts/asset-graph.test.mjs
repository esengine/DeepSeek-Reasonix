import assert from "node:assert/strict";
import test from "node:test";
import { edgePath, layoutAssetGraph, relatedAssetIds } from "./asset-graph.mjs";

const graph = {
  nodes: [
    { id: "IP-A", title: "A" },
    { id: "IP-B", title: "B" },
    { id: "IP-C", title: "C" },
    { id: "IP-D", title: "D" },
  ],
  edges: [
    { id: "REL-AB", sourceAssetId: "IP-A", targetAssetId: "IP-B" },
    { id: "REL-BC", sourceAssetId: "IP-B", targetAssetId: "IP-C" },
  ],
};

test("creates deterministic bounded graph positions with a stable focus", () => {
  const first = layoutAssetGraph(graph, { width: 900, height: 500, focusId: "IP-B" });
  const second = layoutAssetGraph(graph, { width: 900, height: 500, focusId: "IP-B" });
  assert.deepEqual(first, second);
  assert.deepEqual(first.find((node) => node.id === "IP-B"), { id: "IP-B", title: "B", x: 450, y: 250, degree: 2, isFocus: true });
  assert.ok(first.every((node) => node.x >= 68 && node.x <= 832 && node.y >= 68 && node.y <= 432));
});

test("returns bounded neighborhoods without pulling disconnected nodes", () => {
  assert.deepEqual([...relatedAssetIds(graph, "IP-A", 1)].sort(), ["IP-A", "IP-B"]);
  assert.deepEqual([...relatedAssetIds(graph, "IP-A", 2)].sort(), ["IP-A", "IP-B", "IP-C"]);
  assert.deepEqual([...relatedAssetIds(graph, "IP-MISSING", 2)], []);
});

test("builds a finite quadratic edge path", () => {
  assert.match(edgePath({ x: 10, y: 20 }, { x: 200, y: 180 }, "REL-AB"), /^M 10 20 Q [-\d.]+ [-\d.]+ 200 180$/);
});
