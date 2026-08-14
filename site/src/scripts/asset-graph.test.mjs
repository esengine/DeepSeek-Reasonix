import assert from "node:assert/strict";
import test from "node:test";
import {
  edgePath,
  graphZoomLevel,
  layoutAssetGraph,
  normalizeGraphCamera,
  panGraphCamera,
  relatedAssetIds,
  zoomGraphCameraAt,
} from "./asset-graph.mjs";

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

test("normalizes the graph camera within the supported neural panorama range", () => {
  assert.deepEqual(normalizeGraphCamera({ x: "12", y: -8, scale: 9 }), { x: 12, y: -8, scale: 2.4 });
  assert.deepEqual(normalizeGraphCamera({ x: Number.NaN, y: Infinity, scale: 0 }), { x: 0, y: 0, scale: 0.35 });
});

test("zooms around a stable pointer anchor and supports deterministic panning", () => {
  const camera = { x: 42, y: -18, scale: 1.2 };
  const anchor = { x: 360, y: 220 };
  const worldBefore = { x: (anchor.x - camera.x) / camera.scale, y: (anchor.y - camera.y) / camera.scale };
  const zoomed = zoomGraphCameraAt(camera, 1.8, anchor);
  const worldAfter = { x: (anchor.x - zoomed.x) / zoomed.scale, y: (anchor.y - zoomed.y) / zoomed.scale };
  assert.ok(Math.abs(worldBefore.x - worldAfter.x) < 0.0001);
  assert.ok(Math.abs(worldBefore.y - worldAfter.y) < 0.0001);
  assert.deepEqual(panGraphCamera(zoomed, { x: 24, y: -12 }), { ...zoomed, x: zoomed.x + 24, y: zoomed.y - 12 });
});

test("maps camera scale to three semantic information levels", () => {
  assert.equal(graphZoomLevel(0.35), "overview");
  assert.equal(graphZoomLevel(1), "network");
  assert.equal(graphZoomLevel(2.4), "detail");
});
