import { createHash, randomUUID } from "node:crypto";
import { mkdirSync } from "node:fs";
import path from "node:path";
import Database from "better-sqlite3";
import { createAssetGraphStore } from "./asset-graph-store.mjs";

const ROLES = new Set(["owner", "admin", "editor", "viewer"]);
const TERMINAL_JOB_STATES = new Set(["complete", "failed", "interrupted", "cancelled", "blocked"]);
const TERMINAL_AGENT_TASK_STATES = new Set(["complete", "needs_review", "failed", "interrupted", "cancelled", "blocked"]);
const SEMANTIC_REVIEW_STATUSES = new Set(["pending", "confirmed", "dismissed"]);
const SEMANTIC_CONFLICT_FIELDS = new Set(["owner", "sensitivity", "type"]);

function nowIso() {
  return new Date().toISOString();
}

function parseJson(value, fallback = null) {
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

function clone(value) {
  return value == null ? value : structuredClone(value);
}

function boundedText(value, limit) {
  return String(value ?? "").normalize("NFKC").replace(/\s+/g, " ").trim().slice(0, limit);
}

function boundedScore(value) {
  const score = Number(value);
  return Number.isFinite(score) ? Math.max(0, Math.min(1, score)) : 0;
}

function uniqueBoundedStrings(values, limit, itemLimit) {
  return [...new Set((Array.isArray(values) ? values : []).map((value) => boundedText(value, itemLimit)).filter(Boolean))].slice(0, limit);
}

function formatWikiVersion(sequence) {
  return `V1.${Math.max(0, Number(sequence) - 1)}`;
}

function contentFromAsset(asset) {
  return {
    title: String(asset?.wiki?.title || asset?.title || "未命名 Wiki"),
    executiveSummary: String(asset?.wiki?.executiveSummary || asset?.summary || ""),
    keyMechanism: String(asset?.wiki?.keyMechanism || ""),
    metrics: Array.isArray(asset?.wiki?.metrics) ? asset.wiki.metrics : [],
    relationships: Array.isArray(asset?.wiki?.relationships) ? asset.wiki.relationships : [],
  };
}

function publicSessionRow(row) {
  if (!row) return null;
  return {
    id: row.session_id,
    expiresAt: row.expires_at,
    user: {
      id: row.user_id,
      email: row.email,
      name: row.user_name,
      role: row.role,
    },
    workspace: {
      id: row.workspace_id,
      name: row.workspace_name,
    },
  };
}

function publicMemberRow(row) {
  const user = rowToPublicUser(row);
  return user ? { ...user, status: user.disabledAt ? "disabled" : "active", lastLoginAt: row.last_login_at ?? null } : null;
}

function publicAuditRow(row) {
  if (!row) return null;
  return {
    id: row.id,
    action: row.action,
    objectType: row.object_type,
    objectId: row.object_id,
    detail: parseJson(row.detail_json, {}),
    previousHash: row.previous_hash,
    eventHash: row.event_hash,
    createdAt: row.created_at,
    actor: row.actor_user_id ? { id: row.actor_user_id, name: row.actor_name || "已停用用户" } : null,
  };
}

function rowToPublicUser(row) {
  if (!row) return null;
  return {
    id: row.id,
    workspaceId: row.workspace_id,
    email: row.email,
    name: row.name,
    role: row.role,
    createdAt: row.created_at,
    disabledAt: row.disabled_at ?? null,
  };
}

function invitationRow(row) {
  if (!row) return null;
  return {
    id: row.id,
    workspaceId: row.workspace_id,
    email: row.email,
    name: row.name,
    role: row.role,
    invitedBy: row.invited_by,
    createdAt: row.created_at,
    expiresAt: row.expires_at,
    acceptedAt: row.accepted_at ?? null,
    revokedAt: row.revoked_at ?? null,
    status: row.accepted_at ? "accepted" : row.revoked_at ? "revoked" : Date.parse(row.expires_at) <= Date.now() ? "expired" : "pending",
  };
}

function shareRow(row) {
  if (!row) return null;
  return {
    id: row.id,
    workspaceId: row.workspace_id,
    assetId: row.asset_id,
    recipientEmail: row.recipient_email,
    scope: row.scope,
    createdBy: row.created_by,
    createdAt: row.created_at,
    expiresAt: row.expires_at,
    revokedAt: row.revoked_at ?? null,
    accessCount: Number(row.access_count || 0),
    lastAccessedAt: row.last_accessed_at ?? null,
    status: row.revoked_at ? "revoked" : Date.parse(row.expires_at) <= Date.now() ? "expired" : "active",
  };
}

function agentTaskEventRow(row) {
  if (!row) return null;
  return {
    id: row.id,
    taskId: row.task_id,
    type: row.event_type,
    stepId: row.step_id ?? null,
    detail: parseJson(row.detail_json, {}),
    createdAt: row.created_at,
  };
}

export function createPlatformStore(options = {}) {
  const dbPath = path.resolve(options.dbPath ?? path.resolve(process.cwd(), ".runtime", "intelifar.sqlite"));
  mkdirSync(path.dirname(dbPath), { recursive: true });
  const database = new Database(dbPath);
  database.pragma("foreign_keys = ON");
  database.pragma("journal_mode = WAL");
  database.pragma("busy_timeout = 5000");
  database.pragma("synchronous = NORMAL");
  database.exec(`
    CREATE TABLE IF NOT EXISTS workspaces (
      id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      created_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      email TEXT NOT NULL COLLATE NOCASE,
      name TEXT NOT NULL,
      role TEXT NOT NULL CHECK(role IN ('owner','admin','editor','viewer')),
      password_hash TEXT NOT NULL,
      created_at TEXT NOT NULL,
      disabled_at TEXT,
      UNIQUE(workspace_id, email)
    );
    CREATE UNIQUE INDEX IF NOT EXISTS users_global_email ON users(email COLLATE NOCASE);
    CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY,
      token_hash TEXT NOT NULL UNIQUE,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      created_at TEXT NOT NULL,
      expires_at TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS sessions_expires ON sessions(expires_at);
    CREATE TABLE IF NOT EXISTS invitations (
      id TEXT PRIMARY KEY,
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      email TEXT NOT NULL COLLATE NOCASE,
      name TEXT NOT NULL,
      role TEXT NOT NULL CHECK(role IN ('admin','editor','viewer')),
      token_hash TEXT NOT NULL UNIQUE,
      invited_by TEXT REFERENCES users(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL,
      expires_at TEXT NOT NULL,
      accepted_at TEXT,
      revoked_at TEXT
    );
    CREATE INDEX IF NOT EXISTS invitations_workspace_created ON invitations(workspace_id, created_at DESC);
    CREATE INDEX IF NOT EXISTS invitations_email ON invitations(workspace_id, email COLLATE NOCASE);
    CREATE TABLE IF NOT EXISTS secure_shares (
      id TEXT PRIMARY KEY,
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      asset_id TEXT NOT NULL,
      recipient_email TEXT NOT NULL COLLATE NOCASE,
      scope TEXT NOT NULL CHECK(scope IN ('redacted-wiki')),
      token_hash TEXT NOT NULL UNIQUE,
      access_code_hash TEXT NOT NULL,
      created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL,
      expires_at TEXT NOT NULL,
      revoked_at TEXT,
      access_count INTEGER NOT NULL DEFAULT 0,
      last_accessed_at TEXT
    );
    CREATE INDEX IF NOT EXISTS secure_shares_workspace_created ON secure_shares(workspace_id, created_at DESC);
    CREATE TABLE IF NOT EXISTS analysis_jobs (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      id TEXT NOT NULL,
      state TEXT NOT NULL,
      payload_json TEXT NOT NULL,
      upload_path TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, id)
    );
    CREATE INDEX IF NOT EXISTS analysis_jobs_workspace_updated ON analysis_jobs(workspace_id, updated_at DESC);
    CREATE TABLE IF NOT EXISTS agent_tasks (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      id TEXT NOT NULL,
      created_by TEXT NOT NULL,
      state TEXT NOT NULL,
      payload_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, id)
    );
    CREATE INDEX IF NOT EXISTS agent_tasks_creator_updated ON agent_tasks(workspace_id, created_by, updated_at DESC);
    CREATE TABLE IF NOT EXISTS agent_task_events (
      sequence INTEGER PRIMARY KEY AUTOINCREMENT,
      id TEXT NOT NULL UNIQUE,
      workspace_id TEXT NOT NULL,
      task_id TEXT NOT NULL,
      event_type TEXT NOT NULL,
      step_id TEXT,
      detail_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      FOREIGN KEY(workspace_id, task_id) REFERENCES agent_tasks(workspace_id, id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS agent_task_events_task_sequence ON agent_task_events(workspace_id, task_id, sequence);
    CREATE TABLE IF NOT EXISTS publications (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      id TEXT NOT NULL,
      source_job_id TEXT NOT NULL,
      payload_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, id),
      UNIQUE(workspace_id, source_job_id)
    );
    CREATE TABLE IF NOT EXISTS wiki_versions (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      asset_id TEXT NOT NULL,
      version_number INTEGER NOT NULL,
      content_json TEXT NOT NULL,
      change_note TEXT NOT NULL DEFAULT '',
      editor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL,
      PRIMARY KEY(workspace_id, asset_id, version_number)
    );
    CREATE INDEX IF NOT EXISTS wiki_versions_asset ON wiki_versions(workspace_id, asset_id, version_number DESC);
    CREATE TABLE IF NOT EXISTS wiki_review_requests (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      id TEXT NOT NULL,
      asset_id TEXT NOT NULL,
      base_version TEXT NOT NULL,
      draft_json TEXT NOT NULL,
      change_note TEXT NOT NULL DEFAULT '',
      status TEXT NOT NULL CHECK(status IN ('pending', 'approved', 'rejected')),
      submitted_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      reviewed_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      review_note TEXT NOT NULL DEFAULT '',
      created_at TEXT NOT NULL,
      reviewed_at TEXT,
      PRIMARY KEY(workspace_id, id)
    );
    CREATE UNIQUE INDEX IF NOT EXISTS wiki_review_pending_asset ON wiki_review_requests(workspace_id, asset_id) WHERE status = 'pending';
    CREATE INDEX IF NOT EXISTS wiki_review_workspace_status ON wiki_review_requests(workspace_id, status, created_at DESC);
    CREATE TABLE IF NOT EXISTS semantic_reviews (
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      id TEXT NOT NULL,
      fingerprint TEXT NOT NULL,
      kind TEXT NOT NULL CHECK(kind IN ('duplicate', 'conflict')),
      payload_json TEXT NOT NULL,
      status TEXT NOT NULL CHECK(status IN ('pending', 'confirmed', 'dismissed')),
      engine TEXT NOT NULL,
      engine_version TEXT NOT NULL,
      detected_at TEXT NOT NULL,
      last_seen_at TEXT NOT NULL,
      reviewed_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      review_note TEXT NOT NULL DEFAULT '',
      reviewed_at TEXT,
      PRIMARY KEY(workspace_id, id),
      UNIQUE(workspace_id, fingerprint)
    );
    CREATE INDEX IF NOT EXISTS semantic_reviews_workspace_status ON semantic_reviews(workspace_id, status, last_seen_at DESC);
    CREATE TABLE IF NOT EXISTS audit_events (
      sequence INTEGER PRIMARY KEY AUTOINCREMENT,
      id TEXT NOT NULL UNIQUE,
      workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      action TEXT NOT NULL,
      object_type TEXT NOT NULL,
      object_id TEXT NOT NULL,
      detail_json TEXT NOT NULL,
      previous_hash TEXT NOT NULL,
      event_hash TEXT NOT NULL,
      created_at TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS audit_workspace_sequence ON audit_events(workspace_id, sequence);
  `);
  const assetGraphStore = createAssetGraphStore(database);

  const statements = {
    ensureWorkspace: database.prepare(`
      INSERT INTO workspaces(id, name, created_at) VALUES(@id, @name, @createdAt)
      ON CONFLICT(id) DO UPDATE SET name = excluded.name
    `),
    createUser: database.prepare(`
      INSERT INTO users(id, workspace_id, email, name, role, password_hash, created_at)
      VALUES(@id, @workspaceId, @email, @name, @role, @passwordHash, @createdAt)
    `),
    getUserByEmail: database.prepare(`
      SELECT id, workspace_id, email, name, role, password_hash, created_at, disabled_at
      FROM users WHERE email = ? COLLATE NOCASE
    `),
    getUserById: database.prepare(`
      SELECT id, workspace_id, email, name, role, password_hash, created_at, disabled_at
      FROM users WHERE id = ?
    `),
    createSession: database.prepare(`
      INSERT INTO sessions(id, token_hash, user_id, created_at, expires_at)
      VALUES(@id, @tokenHash, @userId, @createdAt, @expiresAt)
    `),
    getSession: database.prepare(`
      SELECT s.id AS session_id, s.expires_at, u.id AS user_id, u.email, u.name AS user_name, u.role,
             w.id AS workspace_id, w.name AS workspace_name
      FROM sessions s
      JOIN users u ON u.id = s.user_id
      JOIN workspaces w ON w.id = u.workspace_id
      WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled_at IS NULL
    `),
    deleteSession: database.prepare("DELETE FROM sessions WHERE token_hash = ?"),
    pruneSessions: database.prepare("DELETE FROM sessions WHERE expires_at <= ?"),
    deleteSessionsByUser: database.prepare("DELETE FROM sessions WHERE user_id = ?"),
    listMembers: database.prepare(`
      SELECT u.id, u.workspace_id, u.email, u.name, u.role, u.created_at, u.disabled_at,
             (SELECT MAX(s.created_at) FROM sessions s WHERE s.user_id = u.id) AS last_login_at
      FROM users u WHERE u.workspace_id = ? ORDER BY CASE u.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 WHEN 'editor' THEN 3 ELSE 4 END, u.created_at ASC
    `),
    workspaceMember: database.prepare(`
      SELECT id, workspace_id, email, name, role, password_hash, created_at, disabled_at
      FROM users WHERE workspace_id = ? AND id = ?
    `),
    updateMember: database.prepare("UPDATE users SET role = @role, disabled_at = @disabledAt WHERE workspace_id = @workspaceId AND id = @id"),
    revokeActiveInvitationsForEmail: database.prepare(`
      UPDATE invitations SET revoked_at = @revokedAt
      WHERE workspace_id = @workspaceId AND email = @email COLLATE NOCASE AND accepted_at IS NULL AND revoked_at IS NULL
    `),
    createInvitation: database.prepare(`
      INSERT INTO invitations(id, workspace_id, email, name, role, token_hash, invited_by, created_at, expires_at)
      VALUES(@id, @workspaceId, @email, @name, @role, @tokenHash, @invitedBy, @createdAt, @expiresAt)
    `),
    listInvitations: database.prepare("SELECT * FROM invitations WHERE workspace_id = ? ORDER BY created_at DESC"),
    invitationByToken: database.prepare(`
      SELECT * FROM invitations WHERE token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?
    `),
    invitationByWorkspaceId: database.prepare("SELECT * FROM invitations WHERE workspace_id = ? AND id = ?"),
    acceptInvitation: database.prepare("UPDATE invitations SET accepted_at = @acceptedAt WHERE id = @id AND accepted_at IS NULL AND revoked_at IS NULL"),
    revokeInvitation: database.prepare("UPDATE invitations SET revoked_at = @revokedAt WHERE workspace_id = @workspaceId AND id = @id AND accepted_at IS NULL AND revoked_at IS NULL"),
    createShare: database.prepare(`
      INSERT INTO secure_shares(id, workspace_id, asset_id, recipient_email, scope, token_hash, access_code_hash, created_by, created_at, expires_at)
      VALUES(@id, @workspaceId, @assetId, @recipientEmail, @scope, @tokenHash, @accessCodeHash, @createdBy, @createdAt, @expiresAt)
    `),
    listShares: database.prepare("SELECT * FROM secure_shares WHERE workspace_id = ? ORDER BY created_at DESC"),
    shareByTokenHash: database.prepare("SELECT * FROM secure_shares WHERE token_hash = ?"),
    shareByWorkspaceId: database.prepare("SELECT * FROM secure_shares WHERE workspace_id = ? AND id = ?"),
    revokeShare: database.prepare("UPDATE secure_shares SET revoked_at = @revokedAt WHERE workspace_id = @workspaceId AND id = @id AND revoked_at IS NULL"),
    recordShareAccess: database.prepare("UPDATE secure_shares SET access_count = access_count + 1, last_accessed_at = @accessedAt WHERE workspace_id = @workspaceId AND id = @id AND revoked_at IS NULL"),
    saveJob: database.prepare(`
      INSERT INTO analysis_jobs(workspace_id, id, state, payload_json, upload_path, created_at, updated_at)
      VALUES(@workspaceId, @id, @state, @payloadJson, @uploadPath, @createdAt, @updatedAt)
      ON CONFLICT(workspace_id, id) DO UPDATE SET
        state = excluded.state,
        payload_json = excluded.payload_json,
        upload_path = COALESCE(excluded.upload_path, analysis_jobs.upload_path),
        updated_at = excluded.updated_at
    `),
    getJob: database.prepare("SELECT payload_json, upload_path FROM analysis_jobs WHERE workspace_id = ? AND id = ?"),
    listJobs: database.prepare("SELECT payload_json, upload_path FROM analysis_jobs WHERE workspace_id = ? ORDER BY updated_at DESC LIMIT ?"),
    unfinishedJobs: database.prepare("SELECT workspace_id, id, payload_json, upload_path FROM analysis_jobs"),
    saveAgentTask: database.prepare(`
      INSERT INTO agent_tasks(workspace_id, id, created_by, state, payload_json, created_at, updated_at)
      VALUES(@workspaceId, @id, @createdBy, @state, @payloadJson, @createdAt, @updatedAt)
      ON CONFLICT(workspace_id, id) DO UPDATE SET
        state = excluded.state,
        payload_json = excluded.payload_json,
        updated_at = excluded.updated_at
    `),
    getAgentTask: database.prepare("SELECT payload_json FROM agent_tasks WHERE workspace_id = ? AND id = ?"),
    getAgentTaskForCreator: database.prepare("SELECT payload_json FROM agent_tasks WHERE workspace_id = ? AND id = ? AND created_by = ?"),
    listAgentTasksForCreator: database.prepare("SELECT payload_json FROM agent_tasks WHERE workspace_id = ? AND created_by = ? ORDER BY updated_at DESC LIMIT ?"),
    unfinishedAgentTasks: database.prepare("SELECT workspace_id, id, payload_json FROM agent_tasks"),
    insertAgentTaskEvent: database.prepare(`
      INSERT INTO agent_task_events(id, workspace_id, task_id, event_type, step_id, detail_json, created_at)
      VALUES(@id, @workspaceId, @taskId, @eventType, @stepId, @detailJson, @createdAt)
    `),
    listAgentTaskEvents: database.prepare("SELECT * FROM agent_task_events WHERE workspace_id = ? AND task_id = ? ORDER BY sequence ASC"),
    savePublication: database.prepare(`
      INSERT OR IGNORE INTO publications(workspace_id, id, source_job_id, payload_json, created_at)
      VALUES(@workspaceId, @id, @sourceJobId, @payloadJson, @createdAt)
    `),
    updatePublication: database.prepare(`
      UPDATE publications SET payload_json = @payloadJson
      WHERE workspace_id = @workspaceId AND id = @id
    `),
    publicationByJob: database.prepare("SELECT payload_json FROM publications WHERE workspace_id = ? AND source_job_id = ?"),
    listPublications: database.prepare("SELECT payload_json FROM publications WHERE workspace_id = ? ORDER BY created_at DESC"),
    insertWiki: database.prepare(`
      INSERT OR IGNORE INTO wiki_versions(workspace_id, asset_id, version_number, content_json, change_note, editor_user_id, created_at)
      VALUES(@workspaceId, @assetId, @versionNumber, @contentJson, @changeNote, @editorUserId, @createdAt)
    `),
    currentWiki: database.prepare(`
      SELECT v.*, u.name AS editor_name
      FROM wiki_versions v LEFT JOIN users u ON u.id = v.editor_user_id
      WHERE v.workspace_id = ? AND v.asset_id = ?
      ORDER BY v.version_number DESC LIMIT 1
    `),
    wikiVersions: database.prepare(`
      SELECT v.*, u.name AS editor_name
      FROM wiki_versions v LEFT JOIN users u ON u.id = v.editor_user_id
      WHERE v.workspace_id = ? AND v.asset_id = ?
      ORDER BY v.version_number DESC
    `),
    insertWikiReview: database.prepare(`
      INSERT INTO wiki_review_requests(workspace_id, id, asset_id, base_version, draft_json, change_note, status, submitted_by_user_id, created_at)
      VALUES(@workspaceId, @id, @assetId, @baseVersion, @draftJson, @changeNote, 'pending', @submittedByUserId, @createdAt)
    `),
    wikiReviewById: database.prepare(`
      SELECT r.*, submitter.name AS submitted_by_name, reviewer.name AS reviewed_by_name
      FROM wiki_review_requests r
      LEFT JOIN users submitter ON submitter.id = r.submitted_by_user_id
      LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by_user_id
      WHERE r.workspace_id = ? AND r.id = ?
    `),
    listWikiReviews: database.prepare(`
      SELECT r.*, submitter.name AS submitted_by_name, reviewer.name AS reviewed_by_name
      FROM wiki_review_requests r
      LEFT JOIN users submitter ON submitter.id = r.submitted_by_user_id
      LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by_user_id
      WHERE r.workspace_id = ? AND (? = '' OR r.status = ?)
      ORDER BY CASE r.status WHEN 'pending' THEN 0 ELSE 1 END, r.created_at DESC
    `),
    decideWikiReview: database.prepare(`
      UPDATE wiki_review_requests
      SET status = @status, reviewed_by_user_id = @reviewerUserId, review_note = @reviewNote, reviewed_at = @reviewedAt
      WHERE workspace_id = @workspaceId AND id = @id AND status = 'pending'
    `),
    upsertSemanticReview: database.prepare(`
      INSERT INTO semantic_reviews(workspace_id, id, fingerprint, kind, payload_json, status, engine, engine_version, detected_at, last_seen_at)
      VALUES(@workspaceId, @id, @fingerprint, @kind, @payloadJson, 'pending', @engine, @engineVersion, @detectedAt, @lastSeenAt)
      ON CONFLICT(workspace_id, fingerprint) DO UPDATE SET
        payload_json = excluded.payload_json,
        engine = excluded.engine,
        engine_version = excluded.engine_version,
        last_seen_at = excluded.last_seen_at
    `),
    semanticReviewById: database.prepare(`
      SELECT r.*, reviewer.name AS reviewed_by_name
      FROM semantic_reviews r LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by_user_id
      WHERE r.workspace_id = ? AND r.id = ?
    `),
    semanticReviewByFingerprint: database.prepare(`
      SELECT r.*, reviewer.name AS reviewed_by_name
      FROM semantic_reviews r LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by_user_id
      WHERE r.workspace_id = ? AND r.fingerprint = ?
    `),
    listSemanticReviews: database.prepare(`
      SELECT r.*, reviewer.name AS reviewed_by_name
      FROM semantic_reviews r LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by_user_id
      WHERE r.workspace_id = ? AND (? = '' OR r.status = ?)
      ORDER BY CASE r.status WHEN 'pending' THEN 0 WHEN 'confirmed' THEN 1 ELSE 2 END, r.last_seen_at DESC
      LIMIT ?
    `),
    decideSemanticReview: database.prepare(`
      UPDATE semantic_reviews
      SET status = @status, reviewed_by_user_id = @reviewerUserId, review_note = @reviewNote, reviewed_at = @reviewedAt
      WHERE workspace_id = @workspaceId AND id = @id AND status = 'pending'
    `),
    lastAudit: database.prepare("SELECT event_hash FROM audit_events WHERE workspace_id = ? ORDER BY sequence DESC LIMIT 1"),
    insertAudit: database.prepare(`
      INSERT INTO audit_events(id, workspace_id, actor_user_id, action, object_type, object_id, detail_json, previous_hash, event_hash, created_at)
      VALUES(@id, @workspaceId, @actorUserId, @action, @objectType, @objectId, @detailJson, @previousHash, @eventHash, @createdAt)
    `),
    listAudit: database.prepare("SELECT * FROM audit_events WHERE workspace_id = ? ORDER BY sequence ASC"),
    listAuditPublic: database.prepare(`
      SELECT a.*, u.name AS actor_name
      FROM audit_events a LEFT JOIN users u ON u.id = a.actor_user_id
      WHERE a.workspace_id = ? ORDER BY a.sequence DESC LIMIT ?
    `),
  };

  function rowToUser(row) {
    if (!row) return null;
    return {
      id: row.id,
      workspaceId: row.workspace_id,
      email: row.email,
      name: row.name,
      role: row.role,
      passwordHash: row.password_hash,
      createdAt: row.created_at,
      disabledAt: row.disabled_at,
    };
  }

  function wikiRow(row) {
    if (!row) return null;
    const content = parseJson(row.content_json, {});
    return {
      assetId: row.asset_id,
      version: formatWikiVersion(row.version_number),
      versionNumber: row.version_number,
      ...content,
      changeNote: row.change_note,
      editor: row.editor_user_id ? { id: row.editor_user_id, name: row.editor_name || "已停用用户" } : null,
      updatedAt: row.created_at,
    };
  }

  function wikiReviewRow(row) {
    if (!row) return null;
    const draft = parseJson(row.draft_json, {});
    return {
      id: row.id,
      assetId: row.asset_id,
      assetTitle: String(draft.title || row.asset_id),
      baseVersion: row.base_version,
      draft,
      changeNote: row.change_note,
      status: row.status,
      submittedBy: row.submitted_by_user_id ? { id: row.submitted_by_user_id, name: row.submitted_by_name || "已停用用户" } : null,
      reviewedBy: row.reviewed_by_user_id ? { id: row.reviewed_by_user_id, name: row.reviewed_by_name || "已停用用户" } : null,
      reviewNote: row.review_note,
      createdAt: row.created_at,
      reviewedAt: row.reviewed_at,
    };
  }

  function semanticReviewRow(row) {
    if (!row) return null;
    return {
      id: row.id,
      kind: row.kind,
      payload: parseJson(row.payload_json, {}),
      status: row.status,
      engine: row.engine,
      version: row.engine_version,
      detectedAt: row.detected_at,
      lastSeenAt: row.last_seen_at,
      reviewedBy: row.reviewed_by_user_id ? { id: row.reviewed_by_user_id, name: row.reviewed_by_name || "已停用用户" } : null,
      reviewNote: row.review_note,
      reviewedAt: row.reviewed_at,
    };
  }

  function workspaceAssetMap(workspaceId) {
    const assets = new Map();
    for (const row of statements.listPublications.all(workspaceId)) {
      const item = parseJson(row.payload_json, {});
      for (const asset of item.assets || []) {
        const id = boundedText(asset?.id, 120);
        if (!id) continue;
        assets.set(id, {
          id,
          title: boundedText(asset?.title || asset?.wiki?.title || id, 240),
          sourceName: boundedText(asset?.document?.sourceName || item.document?.sourceName || item.document?.title || "已发布记录", 240),
          publishedAt: boundedText(asset?.publishedAt || item.publishedAt, 40),
        });
      }
    }
    return assets;
  }

  function semanticAssetSummary(asset, value = "") {
    return { id: asset.id, title: asset.title, sourceName: asset.sourceName, publishedAt: asset.publishedAt, ...(value ? { value: boundedText(value, 240) } : {}) };
  }

  function normalizeSemanticCandidates(workspaceId, result) {
    const assets = workspaceAssetMap(workspaceId);
    const candidates = [];
    for (const candidate of (Array.isArray(result?.duplicates) ? result.duplicates : []).slice(0, 50)) {
      const assetIds = [...new Set((Array.isArray(candidate?.assetIds) ? candidate.assetIds : []).map((id) => boundedText(id, 120)).filter(Boolean))].sort();
      if (assetIds.length !== 2 || assetIds.some((id) => !assets.has(id))) throw Object.assign(new Error("Semantic review references an asset outside the current workspace"), { code: "SEMANTIC_REVIEW_INVALID" });
      const payloadAssets = assetIds.map((id) => semanticAssetSummary(assets.get(id)));
      candidates.push({
        kind: "duplicate",
        fingerprint: createHash("sha256").update(["duplicate", ...assetIds].join("\n")).digest("hex"),
        payload: {
          assetIds,
          assets: payloadAssets,
          title: payloadAssets[0].title === payloadAssets[1].title ? payloadAssets[0].title : `${payloadAssets[0].title} ↔ ${payloadAssets[1].title}`,
          similarity: boundedScore(candidate?.similarity),
          confidence: boundedScore(candidate?.confidence),
          reasons: uniqueBoundedStrings(candidate?.reasons, 8, 100),
        },
      });
    }
    for (const conflict of (Array.isArray(result?.conflicts) ? result.conflicts : []).slice(0, 50)) {
      const field = boundedText(conflict?.field, 60);
      if (!SEMANTIC_CONFLICT_FIELDS.has(field)) throw Object.assign(new Error("Semantic conflict field is not supported"), { code: "SEMANTIC_REVIEW_INVALID" });
      const sourceValues = new Map();
      for (const source of (Array.isArray(conflict?.sources) ? conflict.sources : []).slice(0, 20)) {
        const id = boundedText(source?.assetId, 120);
        if (!id || !assets.has(id)) throw Object.assign(new Error("Semantic review references an asset outside the current workspace"), { code: "SEMANTIC_REVIEW_INVALID" });
        sourceValues.set(id, boundedText(source?.value, 240));
      }
      const assetIds = [...sourceValues.keys()].sort();
      if (assetIds.length < 2) throw Object.assign(new Error("Semantic conflict requires at least two current-workspace assets"), { code: "SEMANTIC_REVIEW_INVALID" });
      const values = uniqueBoundedStrings(conflict?.values, 10, 240).sort((left, right) => left.localeCompare(right, "zh-CN"));
      const payloadAssets = assetIds.map((id) => semanticAssetSummary(assets.get(id), sourceValues.get(id)));
      candidates.push({
        kind: "conflict",
        fingerprint: createHash("sha256").update(["conflict", field, ...assetIds, ...values].join("\n")).digest("hex"),
        payload: {
          assetIds,
          assets: payloadAssets,
          title: payloadAssets[0].title,
          field,
          severity: ["low", "medium", "high", "critical"].includes(conflict?.severity) ? conflict.severity : "medium",
          confidence: boundedScore(conflict?.confidence),
          values,
        },
      });
    }
    return candidates.slice(0, 100);
  }

  const seedWikiVersions = database.transaction((workspaceId, publication) => {
    for (const asset of publication.assets ?? []) {
      statements.insertWiki.run({
        workspaceId,
        assetId: asset.id,
        versionNumber: 1,
        contentJson: JSON.stringify(contentFromAsset(asset)),
        changeNote: "由分析结果创建",
        editorUserId: null,
        createdAt: publication.publishedAt || nowIso(),
      });
    }
  });

  const savePublicationRecord = database.transaction((workspaceId, publication) => {
    statements.savePublication.run({
      workspaceId,
      id: publication.publicationId,
      sourceJobId: publication.sourceJobId,
      payloadJson: JSON.stringify(publication),
      createdAt: publication.publishedAt || nowIso(),
    });
    const stored = parseJson(statements.publicationByJob.get(workspaceId, publication.sourceJobId).payload_json);
    seedWikiVersions(workspaceId, stored);
    assetGraphStore.projectPublication(workspaceId, stored);
    return stored;
  });

  function updateAssetMetadataBatchCore(workspaceId, assetIds, input, options = {}) {
    const normalizedIds = [...new Set((Array.isArray(assetIds) ? assetIds : []).map((assetId) => String(assetId || "").trim()).filter(Boolean))];
    const owner = String(input.owner || "").normalize("NFKC").trim().slice(0, 80);
    const sensitivity = String(input.sensitivity || "").normalize("NFKC").trim();
    if (!normalizedIds.length || normalizedIds.length > 50) {
      throw Object.assign(new Error("Between 1 and 50 assets are required"), { code: "INVALID_ASSET_BATCH" });
    }
    if (!owner || /^(?:待确权|待认领|待复核)$/i.test(owner) || !new Set(["公开", "内部", "机密"]).has(sensitivity)) {
      throw Object.assign(new Error("A confirmed owner and sensitivity are required"), { code: "INVALID_ASSET_METADATA" });
    }
    const requested = new Set(normalizedIds);
    const found = new Map();
    const changedPublications = [];
    for (const row of statements.listPublications.all(workspaceId)) {
      const publication = parseJson(row.payload_json);
      let changed = false;
      for (const asset of publication?.assets || []) {
        if (!requested.has(asset.id)) continue;
        asset.owner = owner;
        asset.sensitivity = sensitivity;
        asset.updatedAt = options.updatedAt || nowIso();
        found.set(asset.id, asset);
        changed = true;
      }
      if (changed) changedPublications.push(publication);
    }
    if (found.size !== normalizedIds.length) {
      throw Object.assign(new Error("One or more assets were not found in the current workspace"), { code: "NOT_FOUND" });
    }
    for (const publication of changedPublications) {
      statements.updatePublication.run({ workspaceId, id: publication.publicationId, payloadJson: JSON.stringify(publication) });
      assetGraphStore.projectPublication(workspaceId, publication);
    }
    if (options.audit) appendAuditRecord(workspaceId, { ...options.audit, objectId: options.audit.objectId || (normalizedIds.length === 1 ? normalizedIds[0] : `${normalizedIds.length}-assets`), detail: { ...(options.audit.detail || {}), count: normalizedIds.length, assetIds: normalizedIds, owner, sensitivity } });
    return normalizedIds.map((assetId) => clone(found.get(assetId)));
  }

  const updateAssetMetadataRecord = database.transaction((workspaceId, assetId, input, options = {}) => updateAssetMetadataBatchCore(workspaceId, [assetId], input, options)[0]);
  const updateAssetMetadataBatchRecord = database.transaction(updateAssetMetadataBatchCore);

  function nextWikiContent(current, input) {
    if (String(input.baseVersion || "") !== formatWikiVersion(current.version_number)) {
      const conflict = new Error("Wiki version changed; reload before saving");
      conflict.code = "VERSION_CONFLICT";
      conflict.currentVersion = formatWikiVersion(current.version_number);
      throw conflict;
    }
    const currentContent = parseJson(current.content_json, {});
    const nextContent = {
      title: String(input.title ?? currentContent.title),
      executiveSummary: String(input.executiveSummary ?? currentContent.executiveSummary),
      keyMechanism: String(input.keyMechanism ?? currentContent.keyMechanism),
      metrics: Array.isArray(input.metrics) ? input.metrics : (currentContent.metrics ?? []),
      relationships: Array.isArray(input.relationships) ? input.relationships : (currentContent.relationships ?? []),
    };
    const sameEditableContent = ["title", "executiveSummary", "keyMechanism"]
      .every((key) => String(nextContent[key] ?? "").trim() === String(currentContent[key] ?? "").trim());
    const sameStructuredContent = JSON.stringify(nextContent.metrics) === JSON.stringify(currentContent.metrics ?? [])
      && JSON.stringify(nextContent.relationships) === JSON.stringify(currentContent.relationships ?? []);
    if (sameEditableContent && sameStructuredContent) {
      const unchanged = new Error("Wiki content has not changed");
      unchanged.code = "NO_CHANGES";
      throw unchanged;
    }
    return nextContent;
  }

  function createWikiVersionRecord(workspaceId, assetId, input) {
    const current = statements.currentWiki.get(workspaceId, assetId);
    if (!current) {
      const missing = new Error("Wiki not found");
      missing.code = "NOT_FOUND";
      throw missing;
    }
    const nextContent = nextWikiContent(current, input);
    statements.insertWiki.run({
      workspaceId,
      assetId,
      versionNumber: current.version_number + 1,
      contentJson: JSON.stringify(nextContent),
      changeNote: String(input.changeNote || "内容更新"),
      editorUserId: input.editorUserId || null,
      createdAt: nowIso(),
    });
    return wikiRow(statements.currentWiki.get(workspaceId, assetId));
  }

  const createWikiVersion = database.transaction(createWikiVersionRecord);

  const submitWikiReviewRecord = database.transaction((workspaceId, assetId, input) => {
    const current = statements.currentWiki.get(workspaceId, assetId);
    if (!current) throw Object.assign(new Error("Wiki not found"), { code: "NOT_FOUND" });
    const draft = nextWikiContent(current, input);
    const id = `WREV-${randomUUID()}`;
    try {
      statements.insertWikiReview.run({ workspaceId, id, assetId, baseVersion: input.baseVersion, draftJson: JSON.stringify(draft), changeNote: String(input.changeNote || "内容更新"), submittedByUserId: input.submittedByUserId || null, createdAt: nowIso() });
    } catch (error) {
      if (/UNIQUE constraint failed/i.test(String(error?.message || ""))) throw Object.assign(new Error("A pending Wiki review already exists"), { code: "REVIEW_PENDING" });
      throw error;
    }
    return wikiReviewRow(statements.wikiReviewById.get(workspaceId, id));
  });

  const decideWikiReviewRecord = database.transaction((workspaceId, reviewId, input) => {
    const row = statements.wikiReviewById.get(workspaceId, reviewId);
    if (!row) throw Object.assign(new Error("Wiki review not found"), { code: "NOT_FOUND" });
    if (row.status !== "pending") throw Object.assign(new Error("Wiki review already decided"), { code: "REVIEW_DECIDED" });
    const decision = String(input.decision || "");
    if (!["approved", "rejected"].includes(decision)) throw Object.assign(new Error("Invalid Wiki review decision"), { code: "INVALID_DECISION" });
    let wiki = null;
    if (decision === "approved") {
      const draft = parseJson(row.draft_json, {});
      wiki = createWikiVersionRecord(workspaceId, row.asset_id, { ...draft, baseVersion: row.base_version, changeNote: row.change_note, editorUserId: input.reviewerUserId || null });
    }
    statements.decideWikiReview.run({ workspaceId, id: reviewId, status: decision, reviewerUserId: input.reviewerUserId || null, reviewNote: String(input.reviewNote || ""), reviewedAt: nowIso() });
    return { review: wikiReviewRow(statements.wikiReviewById.get(workspaceId, reviewId)), wiki };
  });

  const upsertSemanticReviewRecords = database.transaction((workspaceId, result, options = {}) => {
    const seenAt = options.detectedAt || nowIso();
    const engine = boundedText(result?.engine || "Semantica", 80) || "Semantica";
    const engineVersion = boundedText(result?.version, 40);
    const reviews = [];
    for (const candidate of normalizeSemanticCandidates(workspaceId, result)) {
      const id = `SEMREV-${createHash("sha256").update(`${workspaceId}\n${candidate.fingerprint}`).digest("hex").slice(0, 24).toUpperCase()}`;
      statements.upsertSemanticReview.run({
        workspaceId,
        id,
        fingerprint: candidate.fingerprint,
        kind: candidate.kind,
        payloadJson: JSON.stringify(candidate.payload),
        engine,
        engineVersion,
        detectedAt: seenAt,
        lastSeenAt: seenAt,
      });
      reviews.push(semanticReviewRow(statements.semanticReviewByFingerprint.get(workspaceId, candidate.fingerprint)));
    }
    return reviews;
  });

  const decideSemanticReviewRecord = database.transaction((workspaceId, reviewId, input, options = {}) => {
    const row = statements.semanticReviewById.get(workspaceId, reviewId);
    if (!row) throw Object.assign(new Error("Semantic review not found"), { code: "NOT_FOUND" });
    if (row.status !== "pending") throw Object.assign(new Error("Semantic review already decided"), { code: "SEMANTIC_REVIEW_DECIDED" });
    const decision = boundedText(input?.decision, 20);
    if (!new Set(["confirmed", "dismissed"]).has(decision)) throw Object.assign(new Error("Invalid semantic review decision"), { code: "INVALID_DECISION" });
    const reviewNote = boundedText(input?.reviewNote, 300);
    const reviewedAt = input?.reviewedAt || nowIso();
    const reviewerUserId = input?.reviewerUserId || null;
    if (reviewerUserId && !statements.workspaceMember.get(workspaceId, reviewerUserId)) throw Object.assign(new Error("Semantic reviewer is outside the current workspace"), { code: "SEMANTIC_REVIEW_INVALID" });
    statements.decideSemanticReview.run({ workspaceId, id: reviewId, status: decision, reviewerUserId, reviewNote, reviewedAt });
    const decided = semanticReviewRow(statements.semanticReviewById.get(workspaceId, reviewId));
    if (options.audit) {
      appendAuditRecord(workspaceId, {
        ...options.audit,
        objectId: decided.id,
        detail: {
          ...(options.audit.detail || {}),
          decision,
          kind: decided.kind,
          assetIds: decided.payload.assetIds || [],
          field: decided.payload.field || null,
          notePresent: Boolean(reviewNote),
          formalKnowledgeMutation: false,
        },
      });
    }
    return decided;
  });

  const createInvitationRecord = database.transaction((workspaceId, input) => {
    statements.revokeActiveInvitationsForEmail.run({ workspaceId, email: input.email, revokedAt: input.createdAt });
    statements.createInvitation.run({ ...input, workspaceId });
    return invitationRow(statements.invitationByWorkspaceId.get(workspaceId, input.id));
  });

  const acceptInvitationRecord = database.transaction((tokenHash, input) => {
    const invitation = statements.invitationByToken.get(tokenHash, input.acceptedAt);
    if (!invitation) {
      const error = new Error("Invitation is invalid or expired");
      error.code = "INVITATION_UNAVAILABLE";
      throw error;
    }
    statements.createUser.run({
      id: input.userId,
      workspaceId: invitation.workspace_id,
      email: invitation.email,
      name: invitation.name,
      role: invitation.role,
      passwordHash: input.passwordHash,
      createdAt: input.acceptedAt,
    });
    if (statements.acceptInvitation.run({ id: invitation.id, acceptedAt: input.acceptedAt }).changes !== 1) {
      const error = new Error("Invitation is invalid or expired");
      error.code = "INVITATION_UNAVAILABLE";
      throw error;
    }
    return { user: publicMemberRow(statements.getUserById.get(input.userId)), invitation: invitationRow({ ...invitation, accepted_at: input.acceptedAt }) };
  });

  const updateMemberRecord = database.transaction((workspaceId, id, input) => {
    const current = statements.workspaceMember.get(workspaceId, id);
    if (!current) return null;
    if (current.role === "owner") {
      const error = new Error("Workspace owner cannot be disabled or reassigned");
      error.code = "OWNER_PROTECTED";
      throw error;
    }
    if (!new Set(["admin", "editor", "viewer"]).has(input.role)) {
      const error = new Error("Unsupported member role");
      error.code = "INVALID_ROLE";
      throw error;
    }
    const disabledAt = input.disabled ? input.updatedAt : null;
    statements.updateMember.run({ workspaceId, id, role: input.role, disabledAt });
    if (disabledAt) statements.deleteSessionsByUser.run(id);
    return publicMemberRow(statements.workspaceMember.get(workspaceId, id));
  });

  function appendAuditRecord(workspaceId, input) {
    const previousHash = statements.lastAudit.get(String(workspaceId))?.event_hash || "0".repeat(64);
    const event = {
      id: input.id || `AUD-${randomUUID()}`,
      workspaceId: String(workspaceId),
      actorUserId: input.actorUserId || null,
      action: String(input.action),
      objectType: String(input.objectType),
      objectId: String(input.objectId),
      detailJson: JSON.stringify(input.detail ?? {}),
      previousHash,
      createdAt: input.createdAt || nowIso(),
    };
    event.eventHash = createHash("sha256").update([event.previousHash, event.workspaceId, event.actorUserId || "", event.action, event.objectType, event.objectId, event.detailJson, event.createdAt].join("\n")).digest("hex");
    statements.insertAudit.run(event);
    return { id: event.id, action: event.action, objectType: event.objectType, objectId: event.objectId, detail: parseJson(event.detailJson, {}), previousHash: event.previousHash, eventHash: event.eventHash, createdAt: event.createdAt };
  }

  return {
    unsafeDatabaseForTests: database,
    ensureWorkspace(input) {
      const item = { id: String(input.id), name: String(input.name), createdAt: input.createdAt || nowIso() };
      statements.ensureWorkspace.run(item);
      return clone(item);
    },
    createUser(input) {
      if (!ROLES.has(input.role)) throw new Error("Unsupported role");
      const item = {
        id: String(input.id),
        workspaceId: String(input.workspaceId),
        email: String(input.email).trim().toLocaleLowerCase("en-US"),
        name: String(input.name).trim(),
        role: input.role,
        passwordHash: String(input.passwordHash),
        createdAt: input.createdAt || nowIso(),
      };
      statements.createUser.run(item);
      return clone(item);
    },
    getUserByEmail(email) {
      return rowToUser(statements.getUserByEmail.get(String(email).trim().toLocaleLowerCase("en-US")));
    },
    getUserById(id) {
      return rowToUser(statements.getUserById.get(String(id)));
    },
    listMembers(workspaceId) {
      return statements.listMembers.all(String(workspaceId)).map(publicMemberRow);
    },
    updateMember(workspaceId, id, input) {
      return updateMemberRecord(String(workspaceId), String(id), { role: String(input.role), disabled: Boolean(input.disabled), updatedAt: input.updatedAt || nowIso() });
    },
    createInvitation(workspaceId, input) {
      const role = String(input.role);
      if (!new Set(["admin", "editor", "viewer"]).has(role)) {
        const error = new Error("Unsupported invitation role");
        error.code = "INVALID_ROLE";
        throw error;
      }
      const item = {
        id: String(input.id),
        email: String(input.email).trim().toLocaleLowerCase("en-US"),
        name: String(input.name).trim(),
        role,
        tokenHash: String(input.tokenHash),
        invitedBy: input.invitedBy ? String(input.invitedBy) : null,
        createdAt: input.createdAt || nowIso(),
        expiresAt: String(input.expiresAt),
      };
      return createInvitationRecord(String(workspaceId), item);
    },
    listInvitations(workspaceId) {
      return statements.listInvitations.all(String(workspaceId)).map(invitationRow);
    },
    getInvitationByTokenHash(tokenHash, at = nowIso()) {
      return invitationRow(statements.invitationByToken.get(String(tokenHash), String(at)));
    },
    acceptInvitation(tokenHash, input) {
      return acceptInvitationRecord(String(tokenHash), { userId: String(input.userId), passwordHash: String(input.passwordHash), acceptedAt: input.acceptedAt || nowIso() });
    },
    revokeInvitation(workspaceId, id, revokedAt = nowIso()) {
      const changes = statements.revokeInvitation.run({ workspaceId: String(workspaceId), id: String(id), revokedAt: String(revokedAt) }).changes;
      return changes ? invitationRow(statements.invitationByWorkspaceId.get(String(workspaceId), String(id))) : null;
    },
    createShare(workspaceId, input) {
      const item = {
        id: String(input.id),
        workspaceId: String(workspaceId),
        assetId: String(input.assetId),
        recipientEmail: String(input.recipientEmail).trim().toLocaleLowerCase("en-US"),
        scope: "redacted-wiki",
        tokenHash: String(input.tokenHash),
        accessCodeHash: String(input.accessCodeHash),
        createdBy: input.createdBy ? String(input.createdBy) : null,
        createdAt: input.createdAt || nowIso(),
        expiresAt: String(input.expiresAt),
      };
      statements.createShare.run(item);
      return shareRow(statements.shareByWorkspaceId.get(item.workspaceId, item.id));
    },
    listShares(workspaceId) {
      return statements.listShares.all(String(workspaceId)).map(shareRow);
    },
    getShareSecretRecordByTokenHash(tokenHash) {
      const row = statements.shareByTokenHash.get(String(tokenHash));
      return row ? { ...shareRow(row), tokenHash: row.token_hash, accessCodeHash: row.access_code_hash } : null;
    },
    revokeShare(workspaceId, id, revokedAt = nowIso()) {
      const changes = statements.revokeShare.run({ workspaceId: String(workspaceId), id: String(id), revokedAt: String(revokedAt) }).changes;
      return changes ? shareRow(statements.shareByWorkspaceId.get(String(workspaceId), String(id))) : null;
    },
    recordShareAccess(workspaceId, id, accessedAt = nowIso()) {
      const changes = statements.recordShareAccess.run({ workspaceId: String(workspaceId), id: String(id), accessedAt: String(accessedAt) }).changes;
      return changes ? shareRow(statements.shareByWorkspaceId.get(String(workspaceId), String(id))) : null;
    },
    createSession(input) {
      const item = { id: String(input.id), tokenHash: String(input.tokenHash), userId: String(input.userId), createdAt: input.createdAt || nowIso(), expiresAt: String(input.expiresAt) };
      statements.createSession.run(item);
      return { id: item.id, userId: item.userId, createdAt: item.createdAt, expiresAt: item.expiresAt };
    },
    getSession(tokenHash, at = nowIso()) {
      return publicSessionRow(statements.getSession.get(String(tokenHash), at));
    },
    deleteSession(tokenHash) {
      return statements.deleteSession.run(String(tokenHash)).changes > 0;
    },
    pruneSessions(at = nowIso()) {
      return statements.pruneSessions.run(at).changes;
    },
    saveJob(workspaceId, job, optionsForJob = {}) {
      statements.saveJob.run({
        workspaceId: String(workspaceId),
        id: String(job.id),
        state: String(job.state),
        payloadJson: JSON.stringify(job),
        uploadPath: optionsForJob.uploadPath || null,
        createdAt: job.createdAt || nowIso(),
        updatedAt: job.updatedAt || nowIso(),
      });
      return clone(job);
    },
    getJob(workspaceId, id) {
      const row = statements.getJob.get(String(workspaceId), String(id));
      return row ? { job: parseJson(row.payload_json), uploadPath: row.upload_path } : null;
    },
    listJobs(workspaceId, limit = 50) {
      return statements.listJobs.all(String(workspaceId), Math.max(1, Math.min(200, Number(limit) || 50))).map((row) => ({ job: parseJson(row.payload_json), uploadPath: row.upload_path }));
    },
    markInterruptedJobs() {
      let count = 0;
      for (const row of statements.unfinishedJobs.all()) {
        const job = parseJson(row.payload_json, {});
        if (TERMINAL_JOB_STATES.has(job.state)) continue;
        const updated = {
          ...job,
          state: "interrupted",
          stageLabel: "服务重启后等待重试",
          retryable: Boolean(row.upload_path),
          error: "任务在服务重启时中断，可从保留文件安全重试",
          updatedAt: nowIso(),
        };
        statements.saveJob.run({ workspaceId: row.workspace_id, id: row.id, state: updated.state, payloadJson: JSON.stringify(updated), uploadPath: row.upload_path, createdAt: updated.createdAt || updated.updatedAt, updatedAt: updated.updatedAt });
        count += 1;
      }
      return count;
    },
    saveAgentTask(workspaceId, task) {
      const item = clone(task);
      statements.saveAgentTask.run({
        workspaceId: String(workspaceId),
        id: String(item.id),
        createdBy: String(item.createdBy),
        state: String(item.state),
        payloadJson: JSON.stringify(item),
        createdAt: item.createdAt || nowIso(),
        updatedAt: item.updatedAt || nowIso(),
      });
      return clone(item);
    },
    getAgentTask(workspaceId, id, createdBy = null) {
      const row = createdBy == null
        ? statements.getAgentTask.get(String(workspaceId), String(id))
        : statements.getAgentTaskForCreator.get(String(workspaceId), String(id), String(createdBy));
      return row ? parseJson(row.payload_json) : null;
    },
    listAgentTasks(workspaceId, createdBy, limit = 50) {
      return statements.listAgentTasksForCreator
        .all(String(workspaceId), String(createdBy), Math.max(1, Math.min(100, Number(limit) || 50)))
        .map((row) => parseJson(row.payload_json)).filter(Boolean);
    },
    appendAgentTaskEvent(workspaceId, taskId, event) {
      const item = {
        id: String(event.id || `AGE-${randomUUID()}`),
        workspaceId: String(workspaceId),
        taskId: String(taskId),
        eventType: String(event.type),
        stepId: event.stepId == null ? null : String(event.stepId),
        detailJson: JSON.stringify(event.detail ?? {}),
        createdAt: event.createdAt || nowIso(),
      };
      statements.insertAgentTaskEvent.run(item);
      return { id: item.id, taskId: item.taskId, type: item.eventType, stepId: item.stepId, detail: clone(event.detail ?? {}), createdAt: item.createdAt };
    },
    listAgentTaskEvents(workspaceId, taskId) {
      return statements.listAgentTaskEvents.all(String(workspaceId), String(taskId)).map(agentTaskEventRow);
    },
    markInterruptedAgentTasks() {
      let count = 0;
      for (const row of statements.unfinishedAgentTasks.all()) {
        const task = parseJson(row.payload_json, {});
        if (TERMINAL_AGENT_TASK_STATES.has(task.state)) continue;
        const updated = {
          ...task,
          state: "interrupted",
          stageLabel: "服务重启后已安全中断",
          error: "任务在服务重启时中断，未自动重放模型或工具调用",
          updatedAt: nowIso(),
        };
        statements.saveAgentTask.run({ workspaceId: row.workspace_id, id: row.id, createdBy: String(updated.createdBy), state: updated.state, payloadJson: JSON.stringify(updated), createdAt: updated.createdAt || updated.updatedAt, updatedAt: updated.updatedAt });
        count += 1;
      }
      return count;
    },
    savePublication(workspaceId, publication) {
      const item = clone(publication);
      return clone(savePublicationRecord(String(workspaceId), item));
    },
    listPublications(workspaceId) {
      return statements.listPublications.all(String(workspaceId)).map((row) => parseJson(row.payload_json)).filter(Boolean);
    },
    findAsset(workspaceId, assetId) {
      for (const publication of this.listPublications(workspaceId)) {
        const asset = publication.assets?.find((candidate) => candidate.id === assetId);
        if (!asset) continue;
        const wiki = this.getWiki(workspaceId, assetId);
        return wiki ? { ...clone(asset), version: wiki.version, wiki: { title: wiki.title, executiveSummary: wiki.executiveSummary, keyMechanism: wiki.keyMechanism, metrics: wiki.metrics, relationships: wiki.relationships } } : clone(asset);
      }
      return null;
    },
    updateAssetMetadata(workspaceId, assetId, input, options = {}) {
      try {
        return updateAssetMetadataRecord(String(workspaceId), String(assetId), input, options);
      } catch (error) {
        if (error?.code === "NOT_FOUND") return null;
        throw error;
      }
    },
    updateAssetMetadataBatch(workspaceId, assetIds, input, options = {}) {
      return updateAssetMetadataBatchRecord(String(workspaceId), assetIds, input, options);
    },
    getAssetGraph(workspaceId, options = {}) {
      return assetGraphStore.getGraph(String(workspaceId), options);
    },
    searchAssetGraph(workspaceId, query, options = {}) {
      return assetGraphStore.search(String(workspaceId), query, options);
    },
    createAssetRelationship(workspaceId, input, optionsForRelationship = {}) {
      const scopedWorkspaceId = String(workspaceId);
      if (!optionsForRelationship.audit) return assetGraphStore.createRelationship(scopedWorkspaceId, input);
      return database.transaction(() => {
        const relationship = assetGraphStore.createRelationship(scopedWorkspaceId, input);
        appendAuditRecord(scopedWorkspaceId, { ...optionsForRelationship.audit, objectId: relationship.id });
        return relationship;
      })();
    },
    getAssetRelationship(workspaceId, relationshipId) {
      return assetGraphStore.getRelationship(String(workspaceId), String(relationshipId));
    },
    updateAssetRelationshipStatus(workspaceId, relationshipId, status, optionsForRelationship = {}) {
      const scopedWorkspaceId = String(workspaceId);
      if (!optionsForRelationship.audit) return assetGraphStore.updateRelationshipStatus(scopedWorkspaceId, String(relationshipId), String(status));
      return database.transaction(() => {
        const relationship = assetGraphStore.updateRelationshipStatus(scopedWorkspaceId, String(relationshipId), String(status));
        appendAuditRecord(scopedWorkspaceId, { ...optionsForRelationship.audit, objectId: relationship.id });
        return relationship;
      })();
    },
    rebuildAssetGraph(workspaceId) {
      return assetGraphStore.rebuild(String(workspaceId), this.listPublications(workspaceId));
    },
    getWiki(workspaceId, assetId) {
      return wikiRow(statements.currentWiki.get(String(workspaceId), String(assetId)));
    },
    listWikiVersions(workspaceId, assetId) {
      return statements.wikiVersions.all(String(workspaceId), String(assetId)).map(wikiRow);
    },
    saveWikiVersion(workspaceId, assetId, input) {
      return createWikiVersion(String(workspaceId), String(assetId), input);
    },
    submitWikiReview(workspaceId, assetId, input) {
      return submitWikiReviewRecord(String(workspaceId), String(assetId), input);
    },
    listWikiReviews(workspaceId, options = {}) {
      const status = ["pending", "approved", "rejected"].includes(String(options.status || "")) ? String(options.status) : "";
      return statements.listWikiReviews.all(String(workspaceId), status, status).map(wikiReviewRow);
    },
    decideWikiReview(workspaceId, reviewId, input) {
      return decideWikiReviewRecord(String(workspaceId), String(reviewId), input);
    },
    upsertSemanticReviews(workspaceId, result, options = {}) {
      return upsertSemanticReviewRecords(String(workspaceId), result, options);
    },
    listSemanticReviews(workspaceId, options = {}) {
      const status = SEMANTIC_REVIEW_STATUSES.has(String(options.status || "")) ? String(options.status) : "";
      const limit = Math.max(1, Math.min(200, Number(options.limit) || 100));
      return statements.listSemanticReviews.all(String(workspaceId), status, status, limit).map(semanticReviewRow);
    },
    decideSemanticReview(workspaceId, reviewId, input, options = {}) {
      return decideSemanticReviewRecord(String(workspaceId), String(reviewId), input, options);
    },
    appendAudit(workspaceId, input) {
      return appendAuditRecord(String(workspaceId), input);
    },
    listAuditEvents(workspaceId, limit = 100) {
      return statements.listAuditPublic
        .all(String(workspaceId), Math.max(1, Math.min(500, Number(limit) || 100)))
        .map(publicAuditRow);
    },
    verifyAuditChain(workspaceId) {
      const rows = statements.listAudit.all(String(workspaceId));
      let previousHash = "0".repeat(64);
      for (const row of rows) {
        const expected = createHash("sha256").update([previousHash, row.workspace_id, row.actor_user_id || "", row.action, row.object_type, row.object_id, row.detail_json, row.created_at].join("\n")).digest("hex");
        if (row.previous_hash !== previousHash || row.event_hash !== expected) return { valid: false, count: rows.length, failedAt: row.id };
        previousHash = row.event_hash;
      }
      return { valid: true, count: rows.length };
    },
    async backupTo(destination) {
      const target = path.resolve(String(destination));
      mkdirSync(path.dirname(target), { recursive: true });
      return database.backup(target);
    },
    integrityCheck() {
      return { valid: database.pragma("integrity_check", { simple: true }) === "ok", databasePath: dbPath };
    },
    close() {
      database.close();
    },
  };
}
