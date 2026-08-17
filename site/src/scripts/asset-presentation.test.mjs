import assert from "node:assert/strict";
import test from "node:test";

import { collapseAssetGraphForDisplay, groupAssetsForDisplay } from "./asset-presentation.mjs";

const duplicateAssets = [
  { id: "IP-OLD", title: "可溯源 Wiki", type: "技术方案", summary: "同一核心摘要", tags: ["溯源"], updatedAt: "2026-08-10T00:00:00.000Z", evidenceIds: ["EV-OLD"] },
  { id: "IP-NEW", title: "可溯源 Wiki", type: "技术方案", summary: "同一核心摘要", tags: ["版本"], updatedAt: "2026-08-11T00:00:00.000Z", evidenceIds: ["EV-NEW"] },
  { id: "IP-OTHER", title: "安全模型", type: "技术方案", summary: "不同摘要", tags: ["安全"], updatedAt: "2026-08-11T00:00:00.000Z", evidenceIds: ["EV-OTHER"] },
];

test("groups same-title, same-type and same-summary records without deleting lineage", () => {
  const result = groupAssetsForDisplay(duplicateAssets);
  assert.equal(result.rawCount, 3);
  assert.equal(result.uniqueCount, 2);
  assert.equal(result.duplicateRecordCount, 1);
  const wiki = result.assets.find((asset) => asset.title === "可溯源 Wiki");
  assert.equal(wiki.id, "IP-NEW");
  assert.deepEqual(wiki.sourceRecordIds, ["IP-NEW", "IP-OLD"]);
  assert.deepEqual(wiki.tags.sort(), ["溯源", "版本"].sort());
});

test("uses the source document fingerprint when repeated analysis adds more detail", () => {
  const result = groupAssetsForDisplay([
    { id: "IP-A", title: "解析架构", type: "技术方案", summary: "摘要", document: { markdownSha256: "same-document" } },
    { id: "IP-B", title: "解析架构", type: "技术方案", summary: "摘要增加了补充说明", document: { markdownSha256: "same-document" } },
  ]);
  assert.equal(result.uniqueCount, 1);
  assert.equal(result.duplicateRecordCount, 1);
});

test("collapses duplicate graph nodes and remaps relationships to the newest record", () => {
  const graph = collapseAssetGraphForDisplay({
    nodes: duplicateAssets,
    edges: [
      { id: "REL-OLD", sourceAssetId: "IP-OLD", targetAssetId: "IP-OTHER", relationType: "implements", verificationStatus: "proposed", evidenceIds: ["EV-OLD"], confidence: 0.8 },
      { id: "REL-NEW", sourceAssetId: "IP-NEW", targetAssetId: "IP-OTHER", relationType: "implements", verificationStatus: "proposed", evidenceIds: ["EV-NEW"], confidence: 0.9 },
    ],
  });
  assert.equal(graph.nodes.length, 2);
  assert.equal(graph.edges.length, 1);
  assert.equal(graph.edges[0].sourceAssetId, "IP-NEW");
  assert.deepEqual(graph.edges[0].evidenceIds.sort(), ["EV-NEW", "EV-OLD"].sort());
  assert.equal(graph.meta.duplicateRecordCount, 1);
});
