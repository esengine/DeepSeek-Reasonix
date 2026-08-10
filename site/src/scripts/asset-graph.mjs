export const RELATION_LABELS = Object.freeze({
  depends_on: "依赖",
  implements: "实现",
  derived_from: "派生自",
  replaces: "替代",
  references: "引用",
  part_of: "组成",
  similar_to: "相似",
  conflicts_with: "冲突",
});

export const ASSET_TYPE_COLORS = Object.freeze({
  核心技术: "#7258d6",
  技术方案: "#7258d6",
  算法模型: "#466de0",
  软件架构: "#168f82",
  软件著作权: "#168f82",
  评估方法: "#c17d19",
  业务规则: "#b8547b",
  商业秘密: "#b64e52",
  未分类: "#697386",
});

function hashText(value) {
  let hash = 2166136261;
  for (const character of String(value)) {
    hash ^= character.codePointAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function seededUnit(seed) {
  return (hashText(seed) % 10_000) / 10_000;
}

function clamp(value, minimum, maximum) {
  return Math.min(maximum, Math.max(minimum, value));
}

export const GRAPH_CAMERA_LIMITS = Object.freeze({ minimum: 0.35, maximum: 2.4 });

function finiteNumber(value, fallback = 0) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

export function normalizeGraphCamera(camera = {}) {
  return {
    x: finiteNumber(camera.x),
    y: finiteNumber(camera.y),
    scale: clamp(finiteNumber(camera.scale, 1), GRAPH_CAMERA_LIMITS.minimum, GRAPH_CAMERA_LIMITS.maximum),
  };
}

export function zoomGraphCameraAt(camera, nextScale, anchor = { x: 540, y: 310 }) {
  const current = normalizeGraphCamera(camera);
  const scale = normalizeGraphCamera({ scale: nextScale }).scale;
  const point = { x: finiteNumber(anchor.x, 540), y: finiteNumber(anchor.y, 310) };
  const worldX = (point.x - current.x) / current.scale;
  const worldY = (point.y - current.y) / current.scale;
  return { x: point.x - worldX * scale, y: point.y - worldY * scale, scale };
}

export function panGraphCamera(camera, delta = {}) {
  const current = normalizeGraphCamera(camera);
  return {
    ...current,
    x: current.x + finiteNumber(delta.x),
    y: current.y + finiteNumber(delta.y),
  };
}

export function graphZoomLevel(scale) {
  const normalized = normalizeGraphCamera({ scale }).scale;
  if (normalized < 0.72) return "overview";
  if (normalized > 1.45) return "detail";
  return "network";
}

export function relatedAssetIds(graph, rootAssetId, depth = 1) {
  const allowedDepth = clamp(Math.round(Number(depth) || 0), 0, 2);
  const nodeIds = new Set((graph?.nodes ?? []).map((node) => node.id));
  if (!nodeIds.has(rootAssetId)) return new Set();
  const visited = new Set([rootAssetId]);
  let frontier = [rootAssetId];
  for (let hop = 0; hop < allowedDepth && frontier.length; hop += 1) {
    const next = [];
    for (const edge of graph?.edges ?? []) {
      if (frontier.includes(edge.sourceAssetId) && nodeIds.has(edge.targetAssetId) && !visited.has(edge.targetAssetId)) next.push(edge.targetAssetId);
      if (frontier.includes(edge.targetAssetId) && nodeIds.has(edge.sourceAssetId) && !visited.has(edge.sourceAssetId)) next.push(edge.sourceAssetId);
    }
    frontier = [...new Set(next)];
    frontier.forEach((id) => visited.add(id));
  }
  return visited;
}

export function layoutAssetGraph(graph, options = {}) {
  const width = Math.max(480, Number(options.width) || 1_080);
  const height = Math.max(360, Number(options.height) || 620);
  const padding = Math.max(36, Number(options.padding) || 68);
  const nodes = [...(graph?.nodes ?? [])].sort((left, right) => left.id.localeCompare(right.id));
  const edges = graph?.edges ?? [];
  const degree = new Map(nodes.map((node) => [node.id, 0]));
  edges.forEach((edge) => {
    degree.set(edge.sourceAssetId, (degree.get(edge.sourceAssetId) ?? 0) + 1);
    degree.set(edge.targetAssetId, (degree.get(edge.targetAssetId) ?? 0) + 1);
  });
  const focusId = options.focusId && degree.has(options.focusId)
    ? options.focusId
    : nodes.slice().sort((left, right) => (degree.get(right.id) ?? 0) - (degree.get(left.id) ?? 0) || left.id.localeCompare(right.id))[0]?.id;
  const center = { x: width / 2, y: height / 2 };
  const radiusX = Math.max(120, (width - padding * 2) * 0.39);
  const radiusY = Math.max(90, (height - padding * 2) * 0.36);
  const positions = new Map();
  const ringNodes = nodes.filter((node) => node.id !== focusId);
  if (focusId) positions.set(focusId, { id: focusId, x: center.x, y: center.y });
  ringNodes.forEach((node, index) => {
    const angle = (Math.PI * 2 * index) / Math.max(1, ringNodes.length) - Math.PI / 2;
    const jitter = (seededUnit(node.id) - 0.5) * 0.12;
    positions.set(node.id, {
      id: node.id,
      x: center.x + Math.cos(angle + jitter) * radiusX,
      y: center.y + Math.sin(angle + jitter) * radiusY,
    });
  });

  const linked = new Set(edges.flatMap((edge) => [`${edge.sourceAssetId}\u0000${edge.targetAssetId}`, `${edge.targetAssetId}\u0000${edge.sourceAssetId}`]));
  for (let iteration = 0; iteration < 70 && nodes.length > 1; iteration += 1) {
    const cooling = 1 - iteration / 80;
    const deltas = new Map(nodes.map((node) => [node.id, { x: 0, y: 0 }]));
    for (let leftIndex = 0; leftIndex < nodes.length; leftIndex += 1) {
      for (let rightIndex = leftIndex + 1; rightIndex < nodes.length; rightIndex += 1) {
        const left = positions.get(nodes[leftIndex].id);
        const right = positions.get(nodes[rightIndex].id);
        let dx = left.x - right.x;
        let dy = left.y - right.y;
        const distanceSquared = Math.max(180, dx * dx + dy * dy);
        const distance = Math.sqrt(distanceSquared);
        if (distance < 1) { dx = seededUnit(left.id) - 0.5; dy = seededUnit(right.id) - 0.5; }
        const strength = 8_000 / distanceSquared;
        deltas.get(left.id).x += (dx / distance) * strength;
        deltas.get(left.id).y += (dy / distance) * strength;
        deltas.get(right.id).x -= (dx / distance) * strength;
        deltas.get(right.id).y -= (dy / distance) * strength;
      }
    }
    for (const edge of edges) {
      const source = positions.get(edge.sourceAssetId);
      const target = positions.get(edge.targetAssetId);
      if (!source || !target) continue;
      const dx = target.x - source.x;
      const dy = target.y - source.y;
      const distance = Math.max(1, Math.hypot(dx, dy));
      const ideal = linked.has(`${edge.sourceAssetId}\u0000${edge.targetAssetId}`) ? 205 : 240;
      const strength = (distance - ideal) * 0.012;
      deltas.get(source.id).x += (dx / distance) * strength;
      deltas.get(source.id).y += (dy / distance) * strength;
      deltas.get(target.id).x -= (dx / distance) * strength;
      deltas.get(target.id).y -= (dy / distance) * strength;
    }
    for (const node of nodes) {
      if (node.id === focusId) continue;
      const point = positions.get(node.id);
      const delta = deltas.get(node.id);
      delta.x += (center.x - point.x) * 0.0025;
      delta.y += (center.y - point.y) * 0.0025;
      point.x = clamp(point.x + clamp(delta.x, -10, 10) * cooling, padding, width - padding);
      point.y = clamp(point.y + clamp(delta.y, -10, 10) * cooling, padding, height - padding);
    }
  }
  return nodes.map((node) => {
    const point = positions.get(node.id);
    return { ...node, x: Math.round(point.x * 100) / 100, y: Math.round(point.y * 100) / 100, degree: degree.get(node.id) ?? 0, isFocus: node.id === focusId };
  });
}

export function edgePath(source, target, edgeId = "") {
  const dx = target.x - source.x;
  const dy = target.y - source.y;
  const distance = Math.max(1, Math.hypot(dx, dy));
  const curve = (seededUnit(edgeId) - 0.5) * Math.min(90, distance * 0.32);
  const midpointX = (source.x + target.x) / 2 - (dy / distance) * curve;
  const midpointY = (source.y + target.y) / 2 + (dx / distance) * curve;
  return `M ${source.x} ${source.y} Q ${midpointX.toFixed(2)} ${midpointY.toFixed(2)} ${target.x} ${target.y}`;
}
