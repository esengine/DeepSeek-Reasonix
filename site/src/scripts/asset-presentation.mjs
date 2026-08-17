function normalized(value) {
  return String(value ?? "").normalize("NFKC").replace(/\s+/g, " ").trim().toLocaleLowerCase("zh-CN");
}

function assetIdentity(asset) {
  const sourceFingerprint = normalized(asset?.document?.markdownSha256 || asset?.documentSha256 || asset?.summary);
  return [normalized(asset?.title), normalized(asset?.type), sourceFingerprint].join("|");
}

function timestamp(asset) {
  const value = new Date(asset?.updatedAt || asset?.publishedAt || 0).valueOf();
  return Number.isFinite(value) ? value : 0;
}

export function groupAssetsForDisplay(records = []) {
  const groups = new Map();
  for (const record of records) {
    const key = assetIdentity(record) || normalized(record?.id);
    const group = groups.get(key) || [];
    group.push(record);
    groups.set(key, group);
  }

  const assets = [...groups.values()].map((group) => {
    const ordered = group.slice().sort((left, right) => timestamp(right) - timestamp(left));
    const canonical = ordered[0];
    return {
      ...canonical,
      tags: [...new Set(ordered.flatMap((asset) => asset.tags || []))],
      duplicateCount: Math.max(0, ordered.length - 1),
      sourceRecordIds: ordered.map((asset) => asset.id),
      duplicateRecords: ordered.slice(1),
    };
  }).sort((left, right) => timestamp(right) - timestamp(left));

  return {
    assets,
    rawCount: records.length,
    uniqueCount: assets.length,
    duplicateRecordCount: Math.max(0, records.length - assets.length),
    duplicateGroupCount: assets.filter((asset) => asset.duplicateCount > 0).length,
  };
}

export function collapseAssetGraphForDisplay(graph = {}) {
  const sourceNodes = Array.isArray(graph.nodes) ? graph.nodes : [];
  const groups = groupAssetsForDisplay(sourceNodes);
  const canonicalIdById = new Map();
  for (const node of groups.assets) {
    for (const id of node.sourceRecordIds) canonicalIdById.set(id, node.id);
  }

  const nodes = groups.assets.map((node) => ({
    ...node,
    evidenceIds: [...new Set([node, ...(node.duplicateRecords || [])].flatMap((item) => item.evidenceIds || []))],
  }));
  const edgesByKey = new Map();
  for (const edge of Array.isArray(graph.edges) ? graph.edges : []) {
    const sourceAssetId = canonicalIdById.get(edge.sourceAssetId) || edge.sourceAssetId;
    const targetAssetId = canonicalIdById.get(edge.targetAssetId) || edge.targetAssetId;
    if (sourceAssetId === targetAssetId) continue;
    const key = [sourceAssetId, targetAssetId, edge.relationType, edge.verificationStatus].join("|");
    const existing = edgesByKey.get(key);
    if (!existing) {
      edgesByKey.set(key, { ...edge, sourceAssetId, targetAssetId });
      continue;
    }
    existing.evidenceIds = [...new Set([...(existing.evidenceIds || []), ...(edge.evidenceIds || [])])];
    existing.confidence = Math.max(Number(existing.confidence || 0), Number(edge.confidence || 0));
  }

  return {
    ...graph,
    nodes,
    edges: [...edgesByKey.values()],
    meta: {
      ...(graph.meta || {}),
      rawNodeCount: sourceNodes.length,
      duplicateRecordCount: groups.duplicateRecordCount,
      duplicateGroupCount: groups.duplicateGroupCount,
    },
  };
}
