import { createHash, randomUUID } from "node:crypto";

export const ASSET_RELATION_TYPES = Object.freeze([
  "depends_on",
  "implements",
  "derived_from",
  "replaces",
  "references",
  "part_of",
  "similar_to",
  "conflicts_with",
]);

const RELATION_TYPE_SET = new Set(ASSET_RELATION_TYPES);
const SYMMETRIC_RELATIONS = new Set(["similar_to", "conflicts_with"]);
const TERMINAL_RELATION_STATUSES = new Set(["rejected", "superseded"]);
const HIDDEN_FROM_VIEWER = new Set(["机密", "绝密", "confidential", "secret", "restricted"]);

export function normalizeGraphText(value) {
  return String(value ?? "")
    .normalize("NFKC")
    .trim()
    .replace(/\s+/gu, " ")
    .toLocaleLowerCase("zh-CN");
}

function nowIso() {
  return new Date().toISOString();
}

function parseJson(value, fallback) {
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

function clamp(value, minimum, maximum, fallback) {
  if (value == null || value === "") return fallback;
  const number = Number(value);
  return Number.isFinite(number) ? Math.min(maximum, Math.max(minimum, number)) : fallback;
}

function cleanText(value, fallback = "", maxLength = 500) {
  const text = String(value ?? fallback).trim();
  return text.slice(0, maxLength);
}

function mapRelationType(value) {
  const normalized = normalizeGraphText(value).replace(/[\s-]+/gu, "_");
  if (RELATION_TYPE_SET.has(normalized)) return normalized;
  if (/依赖|依存|depends?(_on)?/u.test(normalized)) return "depends_on";
  if (/实现|implements?/u.test(normalized)) return "implements";
  if (/派生|演化|源自|derived(_from)?/u.test(normalized)) return "derived_from";
  if (/替代|取代|replaces?/u.test(normalized)) return "replaces";
  if (/引用|参考|references?|cites?/u.test(normalized)) return "references";
  if (/组成|属于|部分|part(_of)?/u.test(normalized)) return "part_of";
  if (/相似|similar(_to)?/u.test(normalized)) return "similar_to";
  if (/冲突|互斥|conflicts?(_with)?/u.test(normalized)) return "conflicts_with";
  return null;
}

function canonicalEndpoints(sourceAssetId, targetAssetId, relationType) {
  if (!SYMMETRIC_RELATIONS.has(relationType) || sourceAssetId.localeCompare(targetAssetId) <= 0) {
    return [sourceAssetId, targetAssetId];
  }
  return [targetAssetId, sourceAssetId];
}

function deterministicRelationshipId(workspaceId, sourceAssetId, relationType, targetAssetId) {
  const digest = createHash("sha256")
    .update(`${workspaceId}\u0000${sourceAssetId}\u0000${relationType}\u0000${targetAssetId}`)
    .digest("hex")
    .slice(0, 20)
    .toUpperCase();
  return `REL-${digest}`;
}

function isVisibleForRole(node, role) {
  if (role !== "viewer") return true;
  return !HIDDEN_FROM_VIEWER.has(normalizeGraphText(node.sensitivity));
}

function nodeFromRow(row) {
  if (!row) return null;
  return {
    id: row.asset_id,
    publicationId: row.publication_id,
    title: row.title,
    type: row.asset_type,
    owner: row.owner,
    sensitivity: row.sensitivity,
    summary: row.summary,
    tags: parseJson(row.tags_json, []),
    confidence: Number(row.confidence),
    status: row.status,
    version: row.version,
    evidenceIds: parseJson(row.evidence_ids_json, []),
    updatedAt: row.updated_at,
  };
}

export function createAssetGraphStore(database) {
  database.exec(`
    CREATE TABLE IF NOT EXISTS asset_nodes (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      asset_id TEXT NOT NULL,
      publication_id TEXT NOT NULL,
      title TEXT NOT NULL,
      normalized_title TEXT NOT NULL,
      asset_type TEXT NOT NULL,
      owner TEXT NOT NULL,
      sensitivity TEXT NOT NULL,
      summary TEXT NOT NULL,
      tags_json TEXT NOT NULL DEFAULT '[]',
      confidence REAL NOT NULL DEFAULT 0 CHECK(confidence >= 0 AND confidence <= 1),
      status TEXT NOT NULL,
      version TEXT NOT NULL,
      evidence_ids_json TEXT NOT NULL DEFAULT '[]',
      updated_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, asset_id)
    );
    CREATE INDEX IF NOT EXISTS asset_nodes_workspace_title ON asset_nodes(workspace_id, normalized_title);
    CREATE INDEX IF NOT EXISTS asset_nodes_workspace_type ON asset_nodes(workspace_id, asset_type);
    CREATE INDEX IF NOT EXISTS asset_nodes_workspace_sensitivity ON asset_nodes(workspace_id, sensitivity);

    CREATE TABLE IF NOT EXISTS asset_aliases (
      workspace_id TEXT NOT NULL,
      asset_id TEXT NOT NULL,
      alias TEXT NOT NULL,
      normalized_alias TEXT NOT NULL,
      created_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, asset_id, normalized_alias),
      FOREIGN KEY(workspace_id, asset_id) REFERENCES asset_nodes(workspace_id, asset_id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS asset_aliases_lookup ON asset_aliases(workspace_id, normalized_alias);

    CREATE TABLE IF NOT EXISTS asset_relationships (
      workspace_id TEXT NOT NULL,
      relationship_id TEXT NOT NULL,
      source_asset_id TEXT NOT NULL,
      target_asset_id TEXT NOT NULL,
      relation_type TEXT NOT NULL CHECK(relation_type IN ('depends_on','implements','derived_from','replaces','references','part_of','similar_to','conflicts_with')),
      confidence REAL NOT NULL DEFAULT 1 CHECK(confidence >= 0 AND confidence <= 1),
      verification_status TEXT NOT NULL CHECK(verification_status IN ('proposed','confirmed','rejected','superseded')),
      origin TEXT NOT NULL CHECK(origin IN ('manual','model','import')),
      created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      superseded_by TEXT,
      PRIMARY KEY(workspace_id, relationship_id),
      UNIQUE(workspace_id, source_asset_id, target_asset_id, relation_type),
      FOREIGN KEY(workspace_id, source_asset_id) REFERENCES asset_nodes(workspace_id, asset_id) ON DELETE CASCADE,
      FOREIGN KEY(workspace_id, target_asset_id) REFERENCES asset_nodes(workspace_id, asset_id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS asset_relationships_source ON asset_relationships(workspace_id, source_asset_id, verification_status);
    CREATE INDEX IF NOT EXISTS asset_relationships_target ON asset_relationships(workspace_id, target_asset_id, verification_status);
    CREATE INDEX IF NOT EXISTS asset_relationships_type ON asset_relationships(workspace_id, relation_type, verification_status);

    CREATE TABLE IF NOT EXISTS relationship_evidence (
      workspace_id TEXT NOT NULL,
      relationship_id TEXT NOT NULL,
      evidence_id TEXT NOT NULL,
      created_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, relationship_id, evidence_id),
      FOREIGN KEY(workspace_id, relationship_id) REFERENCES asset_relationships(workspace_id, relationship_id) ON DELETE CASCADE
    );
  `);

  const statements = {
    upsertNode: database.prepare(`
      INSERT INTO asset_nodes(
        workspace_id, asset_id, publication_id, title, normalized_title, asset_type, owner,
        sensitivity, summary, tags_json, confidence, status, version, evidence_ids_json, updated_at
      ) VALUES(
        @workspaceId, @assetId, @publicationId, @title, @normalizedTitle, @assetType, @owner,
        @sensitivity, @summary, @tagsJson, @confidence, @status, @version, @evidenceIdsJson, @updatedAt
      ) ON CONFLICT(workspace_id, asset_id) DO UPDATE SET
        publication_id = excluded.publication_id,
        title = excluded.title,
        normalized_title = excluded.normalized_title,
        asset_type = excluded.asset_type,
        owner = excluded.owner,
        sensitivity = excluded.sensitivity,
        summary = excluded.summary,
        tags_json = excluded.tags_json,
        confidence = excluded.confidence,
        status = excluded.status,
        version = excluded.version,
        evidence_ids_json = excluded.evidence_ids_json,
        updated_at = excluded.updated_at
    `),
    upsertAlias: database.prepare(`
      INSERT INTO asset_aliases(workspace_id, asset_id, alias, normalized_alias, created_at)
      VALUES(@workspaceId, @assetId, @alias, @normalizedAlias, @createdAt)
      ON CONFLICT(workspace_id, asset_id, normalized_alias) DO UPDATE SET alias = excluded.alias
    `),
    nodesForWorkspace: database.prepare("SELECT * FROM asset_nodes WHERE workspace_id = ? ORDER BY updated_at DESC, asset_id ASC"),
    nodeById: database.prepare("SELECT * FROM asset_nodes WHERE workspace_id = ? AND asset_id = ?"),
    aliasesForWorkspace: database.prepare("SELECT asset_id, alias, normalized_alias FROM asset_aliases WHERE workspace_id = ? ORDER BY asset_id, normalized_alias"),
    upsertProposedRelationship: database.prepare(`
      INSERT INTO asset_relationships(
        workspace_id, relationship_id, source_asset_id, target_asset_id, relation_type, confidence,
        verification_status, origin, created_by, created_at, updated_at, superseded_by
      ) VALUES(
        @workspaceId, @relationshipId, @sourceAssetId, @targetAssetId, @relationType, @confidence,
        'proposed', 'model', NULL, @createdAt, @updatedAt, NULL
      ) ON CONFLICT(workspace_id, source_asset_id, target_asset_id, relation_type) DO UPDATE SET
        confidence = excluded.confidence,
        updated_at = excluded.updated_at
      WHERE asset_relationships.origin = 'model' AND asset_relationships.verification_status = 'proposed'
    `),
    insertRelationship: database.prepare(`
      INSERT INTO asset_relationships(
        workspace_id, relationship_id, source_asset_id, target_asset_id, relation_type, confidence,
        verification_status, origin, created_by, created_at, updated_at, superseded_by
      ) VALUES(
        @workspaceId, @relationshipId, @sourceAssetId, @targetAssetId, @relationType, @confidence,
        @verificationStatus, @origin, @createdBy, @createdAt, @updatedAt, NULL
      )
    `),
    relationshipById: database.prepare("SELECT * FROM asset_relationships WHERE workspace_id = ? AND relationship_id = ?"),
    relationshipsForWorkspace: database.prepare(`
      SELECT * FROM asset_relationships
      WHERE workspace_id = ? AND verification_status NOT IN ('rejected','superseded')
      ORDER BY updated_at DESC, relationship_id ASC
    `),
    relationshipsForNode: database.prepare(`
      SELECT * FROM asset_relationships
      WHERE workspace_id = ? AND source_asset_id = ? AND verification_status NOT IN ('rejected','superseded')
      UNION ALL
      SELECT * FROM asset_relationships
      WHERE workspace_id = ? AND target_asset_id = ? AND verification_status NOT IN ('rejected','superseded')
      ORDER BY updated_at DESC, relationship_id ASC
    `),
    updateRelationshipStatus: database.prepare(`
      UPDATE asset_relationships SET verification_status = @status, updated_at = @updatedAt
      WHERE workspace_id = @workspaceId AND relationship_id = @relationshipId
    `),
    insertEvidence: database.prepare(`
      INSERT OR IGNORE INTO relationship_evidence(workspace_id, relationship_id, evidence_id, created_at)
      VALUES(@workspaceId, @relationshipId, @evidenceId, @createdAt)
    `),
    evidenceForRelationship: database.prepare(`
      SELECT evidence_id FROM relationship_evidence
      WHERE workspace_id = ? AND relationship_id = ? ORDER BY evidence_id
    `),
  };

  function aliasesIndex(workspaceId) {
    const index = new Map();
    const add = (normalized, assetId) => {
      if (!normalized) return;
      const entries = index.get(normalized) ?? new Set();
      entries.add(assetId);
      index.set(normalized, entries);
    };
    for (const row of statements.nodesForWorkspace.all(workspaceId)) add(row.normalized_title, row.asset_id);
    for (const row of statements.aliasesForWorkspace.all(workspaceId)) add(row.normalized_alias, row.asset_id);
    return index;
  }

  function resolveAssetReference(reference, workspaceNodes, aliasLookup) {
    const raw = cleanText(reference, "", 200);
    if (!raw) return null;
    if (workspaceNodes.has(raw)) return raw;
    const candidates = aliasLookup.get(normalizeGraphText(raw));
    return candidates?.size === 1 ? [...candidates][0] : null;
  }

  const projectPublicationTransaction = database.transaction((workspaceId, publication) => {
    const timestamp = publication.publishedAt || nowIso();
    let projectedNodes = 0;
    for (const asset of publication.assets ?? []) {
      const assetId = cleanText(asset?.id, "", 100);
      const title = cleanText(asset?.title || asset?.wiki?.title, assetId || "未命名资产", 300);
      if (!assetId || !title) continue;
      const evidenceIds = [...new Set((asset.evidence ?? []).map((entry) => cleanText(entry?.id, "", 100)).filter(Boolean))];
      statements.upsertNode.run({
        workspaceId,
        assetId,
        publicationId: cleanText(publication.publicationId, "", 120),
        title,
        normalizedTitle: normalizeGraphText(title),
        assetType: cleanText(asset.type || asset.category, "未分类", 100),
        owner: cleanText(asset.owner, "待认领", 120),
        sensitivity: cleanText(asset.sensitivity, "内部", 50),
        summary: cleanText(asset.wiki?.executiveSummary || asset.summary, "", 2000),
        tagsJson: JSON.stringify(Array.isArray(asset.tags) ? asset.tags.map((tag) => cleanText(tag, "", 80)).filter(Boolean).slice(0, 30) : []),
        confidence: clamp(asset.confidence, 0, 1, 0),
        status: cleanText(asset.status, "待复核", 50),
        version: cleanText(asset.version || publication.version, "V1.0", 50),
        evidenceIdsJson: JSON.stringify(evidenceIds),
        updatedAt: timestamp,
      });
      const aliases = new Set([title, asset.wiki?.title, ...(Array.isArray(asset.aliases) ? asset.aliases : [])]);
      for (const aliasValue of aliases) {
        const alias = cleanText(aliasValue, "", 300);
        const normalizedAlias = normalizeGraphText(alias);
        if (!alias || !normalizedAlias) continue;
        statements.upsertAlias.run({ workspaceId, assetId, alias, normalizedAlias, createdAt: timestamp });
      }
      projectedNodes += 1;
    }

    const workspaceNodes = new Set(statements.nodesForWorkspace.all(workspaceId).map((row) => row.asset_id));
    const aliasLookup = aliasesIndex(workspaceId);
    const publicationAliases = new Map();
    const addPublicationAlias = (value, assetId) => {
      const normalized = normalizeGraphText(value);
      if (!normalized) return;
      const ids = publicationAliases.get(normalized) ?? new Set();
      ids.add(assetId);
      publicationAliases.set(normalized, ids);
    };
    for (const asset of publication.assets ?? []) {
      const assetId = cleanText(asset?.id, "", 100);
      if (!assetId) continue;
      addPublicationAlias(assetId, assetId);
      addPublicationAlias(asset.title, assetId);
      addPublicationAlias(asset.wiki?.title, assetId);
      for (const alias of Array.isArray(asset.aliases) ? asset.aliases : []) addPublicationAlias(alias, assetId);
    }
    const resolvePublicationReference = (reference) => {
      const candidates = publicationAliases.get(normalizeGraphText(reference));
      return candidates?.size === 1 ? [...candidates][0] : null;
    };
    let projectedRelationships = 0;
    const processed = new Set();
    for (const asset of publication.assets ?? []) {
      for (const relationship of asset?.wiki?.relationships ?? []) {
        const relationType = mapRelationType(relationship?.relation || relationship?.type || relationship?.relationType);
        if (!relationType) continue;
        const sourceReference = relationship?.source || asset.id;
        const resolvedSource = resolvePublicationReference(sourceReference) || resolveAssetReference(sourceReference, workspaceNodes, aliasLookup);
        const resolvedTarget = resolvePublicationReference(relationship?.target) || resolveAssetReference(relationship?.target, workspaceNodes, aliasLookup);
        if (!resolvedSource || !resolvedTarget || resolvedSource === resolvedTarget) continue;
        const [sourceAssetId, targetAssetId] = canonicalEndpoints(resolvedSource, resolvedTarget, relationType);
        const relationshipId = deterministicRelationshipId(workspaceId, sourceAssetId, relationType, targetAssetId);
        if (processed.has(relationshipId)) continue;
        processed.add(relationshipId);
        statements.upsertProposedRelationship.run({
          workspaceId,
          relationshipId,
          sourceAssetId,
          targetAssetId,
          relationType,
          confidence: clamp(relationship?.confidence, 0, 1, clamp(asset.confidence, 0, 1, 0.7)),
          createdAt: timestamp,
          updatedAt: timestamp,
        });
        const allowedEvidence = new Set([
          ...parseJson(statements.nodeById.get(workspaceId, sourceAssetId)?.evidence_ids_json, []),
          ...parseJson(statements.nodeById.get(workspaceId, targetAssetId)?.evidence_ids_json, []),
        ]);
        const requestedEvidence = Array.isArray(relationship?.evidenceIds) && relationship.evidenceIds.length
          ? relationship.evidenceIds
          : [...allowedEvidence].slice(0, 4);
        for (const evidenceId of requestedEvidence) {
          const normalizedEvidenceId = cleanText(evidenceId, "", 100);
          if (normalizedEvidenceId && allowedEvidence.has(normalizedEvidenceId)) statements.insertEvidence.run({ workspaceId, relationshipId, evidenceId: normalizedEvidenceId, createdAt: timestamp });
        }
        projectedRelationships += 1;
      }
    }
    return { projectedNodes, projectedRelationships };
  });

  function relationshipFromRow(row, includeEvidence = true) {
    if (!row) return null;
    return {
      id: row.relationship_id,
      sourceAssetId: row.source_asset_id,
      targetAssetId: row.target_asset_id,
      relationType: row.relation_type,
      confidence: Number(row.confidence),
      verificationStatus: row.verification_status,
      origin: row.origin,
      createdBy: row.created_by ?? null,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      supersededBy: row.superseded_by ?? null,
      evidenceIds: includeEvidence ? statements.evidenceForRelationship.all(row.workspace_id, row.relationship_id).map((entry) => entry.evidence_id) : [],
    };
  }

  function createRelationship(workspaceId, input) {
    const relationType = mapRelationType(input.relationType);
    if (!relationType || !RELATION_TYPE_SET.has(relationType)) {
      const error = new Error("Unsupported asset relationship type");
      error.code = "INVALID_RELATION_TYPE";
      throw error;
    }
    const rawSource = cleanText(input.sourceAssetId, "", 100);
    const rawTarget = cleanText(input.targetAssetId, "", 100);
    const [sourceAssetId, targetAssetId] = canonicalEndpoints(rawSource, rawTarget, relationType);
    const sourceNode = statements.nodeById.get(workspaceId, sourceAssetId);
    const targetNode = statements.nodeById.get(workspaceId, targetAssetId);
    if (!sourceAssetId || !targetAssetId || sourceAssetId === targetAssetId || !sourceNode || !targetNode) {
      const error = new Error("Both relationship endpoints must be distinct assets in the current workspace");
      error.code = "INVALID_RELATION_ENDPOINT";
      throw error;
    }
    const origin = new Set(["manual", "model", "import"]).has(input.origin) ? input.origin : "manual";
    const verificationStatus = input.verificationStatus || (origin === "manual" ? "confirmed" : "proposed");
    if (!new Set(["proposed", "confirmed"]).has(verificationStatus)) {
      const error = new Error("New relationships must be proposed or confirmed");
      error.code = "INVALID_RELATION_STATUS";
      throw error;
    }
    const timestamp = input.createdAt || nowIso();
    const relationshipId = cleanText(input.id, `REL-${randomUUID().toUpperCase()}`, 100);
    const evidenceIds = [...new Set((Array.isArray(input.evidenceIds) ? input.evidenceIds : []).map((id) => cleanText(id, "", 100)).filter(Boolean))].slice(0, 20);
    const allowedEvidence = new Set([...parseJson(sourceNode.evidence_ids_json, []), ...parseJson(targetNode.evidence_ids_json, [])]);
    if (evidenceIds.some((evidenceId) => !allowedEvidence.has(evidenceId))) {
      const error = new Error("Relationship evidence must belong to one of its endpoint assets");
      error.code = "INVALID_RELATION_EVIDENCE";
      throw error;
    }
    const transaction = database.transaction(() => {
      try {
        statements.insertRelationship.run({
          workspaceId,
          relationshipId,
          sourceAssetId,
          targetAssetId,
          relationType,
          confidence: clamp(input.confidence, 0, 1, origin === "manual" ? 1 : 0.7),
          verificationStatus,
          origin,
          createdBy: input.createdBy || null,
          createdAt: timestamp,
          updatedAt: timestamp,
        });
      } catch (error) {
        if (String(error?.code).startsWith("SQLITE_CONSTRAINT_UNIQUE")) {
          error.code = "DUPLICATE_RELATIONSHIP";
        }
        throw error;
      }
      for (const evidenceId of evidenceIds) statements.insertEvidence.run({ workspaceId, relationshipId, evidenceId, createdAt: timestamp });
      return relationshipFromRow(statements.relationshipById.get(workspaceId, relationshipId));
    });
    return transaction();
  }

  function updateRelationshipStatus(workspaceId, relationshipId, status) {
    const current = statements.relationshipById.get(workspaceId, relationshipId);
    if (!current) {
      const error = new Error("Asset relationship not found");
      error.code = "NOT_FOUND";
      throw error;
    }
    const allowed = current.verification_status === "proposed"
      ? new Set(["confirmed", "rejected"])
      : current.verification_status === "confirmed"
        ? new Set(["superseded"])
        : new Set();
    if (!allowed.has(status)) {
      const error = new Error(`Cannot change relationship from ${current.verification_status} to ${status}`);
      error.code = "INVALID_RELATION_TRANSITION";
      throw error;
    }
    statements.updateRelationshipStatus.run({ workspaceId, relationshipId, status, updatedAt: nowIso() });
    return relationshipFromRow(statements.relationshipById.get(workspaceId, relationshipId));
  }

  function edgesForNodes(workspaceId, nodeIds, options = {}) {
    const includeProposed = Boolean(options.includeProposed);
    const requestedRelationTypes = options.requestedRelationTypes ?? new Set();
    const visibleById = options.visibleById;
    const edges = new Map();
    for (const nodeId of new Set(nodeIds)) {
      for (const row of statements.relationshipsForNode.all(workspaceId, nodeId, workspaceId, nodeId)) {
        if (edges.has(row.relationship_id)) continue;
        const edge = relationshipFromRow(row, false);
        if (edge.verificationStatus !== "confirmed" && !includeProposed) continue;
        if (requestedRelationTypes.size && !requestedRelationTypes.has(edge.relationType)) continue;
        if (visibleById && (!visibleById.has(edge.sourceAssetId) || !visibleById.has(edge.targetAssetId))) continue;
        edges.set(edge.id, edge);
      }
    }
    return [...edges.values()];
  }

  function getGraph(workspaceId, options = {}) {
    const role = String(options.role || "viewer");
    const includeProposed = Boolean(options.includeProposed) && role !== "viewer";
    const requestedTypes = new Set((Array.isArray(options.types) ? options.types : []).map(String));
    const requestedRelationTypes = new Set((Array.isArray(options.relationTypes) ? options.relationTypes : []).map(String));
    const nodeLimit = Math.round(clamp(options.limit, 1, 200, 100));
    const edgeLimit = Math.round(clamp(options.edgeLimit, 1, 400, 200));
    const depth = Math.round(clamp(options.depth, 0, 2, 1));
    const allNodes = statements.nodesForWorkspace.all(workspaceId).map(nodeFromRow);
    const visibleNodes = allNodes.filter((node) => isVisibleForRole(node, role) && (!requestedTypes.size || requestedTypes.has(node.type)));
    const visibleById = new Map(visibleNodes.map((node) => [node.id, node]));

    const rootAssetId = cleanText(options.rootAssetId, "", 100);
    let selectedNodes;
    let candidateEdges;
    if (rootAssetId) {
      if (!visibleById.has(rootAssetId)) {
        return { nodes: [], edges: [], meta: { rootAssetId, depth, truncated: false, rootUnavailable: true } };
      }
      const visited = new Set([rootAssetId]);
      let frontier = [rootAssetId];
      const collectedEdges = new Map();
      for (let hop = 0; hop < depth && frontier.length; hop += 1) {
        const next = [];
        const hopEdges = edgesForNodes(workspaceId, frontier, { includeProposed, requestedRelationTypes, visibleById });
        for (const edge of hopEdges) {
          collectedEdges.set(edge.id, edge);
          if (frontier.includes(edge.sourceAssetId) && !visited.has(edge.targetAssetId)) next.push(edge.targetAssetId);
          if (frontier.includes(edge.targetAssetId) && !visited.has(edge.sourceAssetId)) next.push(edge.sourceAssetId);
        }
        frontier = [...new Set(next)];
        for (const id of frontier) visited.add(id);
      }
      selectedNodes = [...visited].map((id) => visibleById.get(id)).filter(Boolean).slice(0, nodeLimit);
      candidateEdges = [...collectedEdges.values()];
    } else {
      selectedNodes = visibleNodes.slice(0, nodeLimit);
      candidateEdges = edgesForNodes(workspaceId, selectedNodes.map((node) => node.id), { includeProposed, requestedRelationTypes, visibleById });
    }
    const selectedIds = new Set(selectedNodes.map((node) => node.id));
    const selectedEdges = candidateEdges
      .filter((edge) => selectedIds.has(edge.sourceAssetId) && selectedIds.has(edge.targetAssetId))
      .slice(0, edgeLimit)
      .map((edge) => ({ ...edge, evidenceIds: statements.evidenceForRelationship.all(workspaceId, edge.id).map((entry) => entry.evidence_id) }));
    return {
      nodes: selectedNodes,
      edges: selectedEdges,
      meta: {
        rootAssetId: rootAssetId || null,
        depth,
        totalVisibleNodes: visibleNodes.length,
        totalVisibleEdges: candidateEdges.length,
        truncated: selectedNodes.length < visibleNodes.length || selectedEdges.length < candidateEdges.length,
        rootUnavailable: false,
      },
    };
  }

  function searchGraph(workspaceId, query, options = {}) {
    const normalizedQuery = normalizeGraphText(query).slice(0, 200);
    if (!normalizedQuery) return { query: "", results: [], meta: { directMatches: 0, expandedMatches: 0 } };
    const role = String(options.role || "viewer");
    const depth = Math.round(clamp(options.depth, 0, 2, 1));
    const limit = Math.round(clamp(options.limit, 1, 100, 50));
    const nodes = statements.nodesForWorkspace.all(workspaceId).map(nodeFromRow).filter((node) => isVisibleForRole(node, role));
    const nodesById = new Map(nodes.map((node) => [node.id, node]));
    const aliases = new Map();
    for (const row of statements.aliasesForWorkspace.all(workspaceId)) {
      if (!nodesById.has(row.asset_id)) continue;
      const values = aliases.get(row.asset_id) ?? [];
      values.push(row.normalized_alias);
      aliases.set(row.asset_id, values);
    }
    const direct = [];
    for (const node of nodes) {
      const normalizedId = normalizeGraphText(node.id);
      const normalizedTitle = normalizeGraphText(node.title);
      const searchableDetails = normalizeGraphText([node.summary, node.owner, node.type, ...node.tags, ...(aliases.get(node.id) ?? [])].join(" "));
      let score = 0;
      let explanation = "";
      if (normalizedId === normalizedQuery || normalizedTitle === normalizedQuery) {
        score = 100;
        explanation = "资产编号或名称完全匹配";
      } else if (normalizedTitle.includes(normalizedQuery) || normalizedId.includes(normalizedQuery)) {
        score = 84;
        explanation = "资产名称或编号包含检索词";
      } else if (searchableDetails.includes(normalizedQuery)) {
        score = 66;
        explanation = "资产摘要、标签、类型或别名匹配";
      }
      if (score) direct.push({ asset: node, matchKind: "direct", score, explanation, path: [node.id] });
    }
    direct.sort((left, right) => right.score - left.score || left.asset.title.localeCompare(right.asset.title, "zh-CN"));

    const directIds = new Set(direct.map((result) => result.asset.id));
    const expanded = new Map();
    for (const seed of direct.slice(0, 10)) {
      let frontier = [{ id: seed.asset.id, path: [seed.asset.id], relations: [] }];
      const visited = new Set([seed.asset.id]);
      for (let hop = 1; hop <= depth; hop += 1) {
        const next = [];
        for (const item of frontier) {
          const adjacentEdges = edgesForNodes(workspaceId, [item.id], { includeProposed: false, visibleById: nodesById });
          for (const edge of adjacentEdges) {
            const adjacentId = edge.sourceAssetId === item.id ? edge.targetAssetId : edge.sourceAssetId;
            if (visited.has(adjacentId)) continue;
            visited.add(adjacentId);
            const path = [...item.path, adjacentId];
            const relations = [...item.relations, edge.relationType];
            next.push({ id: adjacentId, path, relations });
            if (!directIds.has(adjacentId)) {
              const current = expanded.get(adjacentId);
              const candidate = {
                asset: nodesById.get(adjacentId),
                matchKind: "graph_expansion",
                score: Math.max(35, seed.score - hop * 18),
                explanation: `通过 ${hop} 跳已确认关系从“${seed.asset.title}”扩展`,
                path,
                relationPath: relations,
              };
              if (!current || candidate.score > current.score) expanded.set(adjacentId, candidate);
            }
          }
        }
        frontier = next;
      }
    }
    const expandedResults = [...expanded.values()].sort((left, right) => right.score - left.score || left.asset.title.localeCompare(right.asset.title, "zh-CN"));
    return {
      query: String(query).slice(0, 200),
      results: [...direct, ...expandedResults].slice(0, limit),
      meta: { directMatches: direct.length, expandedMatches: expandedResults.length, depth },
    };
  }

  return {
    projectPublication(workspaceId, publication) {
      return projectPublicationTransaction(String(workspaceId), publication);
    },
    rebuild(workspaceId, publications) {
      let projectedNodes = 0;
      let projectedRelationships = 0;
      for (const publication of publications) {
        const result = projectPublicationTransaction(String(workspaceId), publication);
        projectedNodes += result.projectedNodes;
        projectedRelationships += result.projectedRelationships;
      }
      return { projectedNodes, projectedRelationships };
    },
    getGraph(workspaceId, options) {
      return getGraph(String(workspaceId), options);
    },
    search(workspaceId, query, options) {
      return searchGraph(String(workspaceId), query, options);
    },
    createRelationship(workspaceId, input) {
      return createRelationship(String(workspaceId), input);
    },
    getRelationship(workspaceId, relationshipId) {
      return relationshipFromRow(statements.relationshipById.get(String(workspaceId), String(relationshipId)));
    },
    updateRelationshipStatus(workspaceId, relationshipId, status) {
      return updateRelationshipStatus(String(workspaceId), String(relationshipId), String(status));
    },
  };
}
