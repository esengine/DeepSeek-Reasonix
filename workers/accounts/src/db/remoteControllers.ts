import type { ControllerJwk } from "../auth/controllerProof";
import {
  REMOTE_CONTROLLER_ACTIVE_CAP,
  REMOTE_CONTROLLER_IDLE_MS,
  REMOTE_CONTROLLER_PENDING_CAP,
  REMOTE_PENDING_TTL_MS,
} from "../config";

export type ControllerState = "pending" | "active" | "revoked";

export const CONTROLLER_UA_NAMES = {
  "ios-safari": "iPhone Safari",
  "ios-webview": "iPhone in-app browser",
  "android-chrome": "Android Chrome",
  "android-webview": "Android in-app browser",
  "desktop-chrome": "Chrome",
  "desktop-safari": "Safari",
  "desktop-firefox": "Firefox",
  "desktop-edge": "Edge",
  other: "Other browser",
} as const;
export type ControllerUaClass = keyof typeof CONTROLLER_UA_NAMES;

export function clampUaClass(value: string): ControllerUaClass {
  return Object.hasOwn(CONTROLLER_UA_NAMES, value) ? (value as ControllerUaClass) : "other";
}

export interface ControllerRow {
  id: string;
  user_id: number;
  host_device_id: string;
  key_thumbprint: string;
  public_key: string;
  ordinal: number | null;
  name: string;
  name_source: "auto" | "owner";
  ua_class: string;
  state: ControllerState;
  claims_id: string | null;
  requester_session_hash: string;
  created_at: string;
  last_seen_at: string;
  revoked_at: string | null;
  expires_at: string | null;
}

export interface ControllerView {
  id: string;
  ordinal: number | null;
  name: string;
  nameSource: "auto" | "owner";
  state: ControllerState;
  uaClass: string;
  claimsId: string | null;
  createdAt: string;
  lastSeenAt: string;
  revokedAt: string | null;
  expiresAt: string | null;
}

export interface HostControllerView extends ControllerView {
  keyThumbprint: string;
  publicKey: ControllerJwk | null;
  replaceCandidate: string | null;
}

export function toControllerView(row: ControllerRow): ControllerView {
  return {
    id: row.id,
    ordinal: row.ordinal,
    name: row.name,
    nameSource: row.name_source,
    state: row.state,
    uaClass: row.ua_class,
    claimsId: row.claims_id,
    createdAt: row.created_at,
    lastSeenAt: row.last_seen_at,
    revokedAt: row.revoked_at,
    expiresAt: row.expires_at,
  };
}

export function toHostControllerView(row: ControllerRow, replaceCandidate: string | null): HostControllerView {
  let publicKey: ControllerJwk | null = null;
  try {
    publicKey = JSON.parse(row.public_key) as ControllerJwk;
  } catch {
    publicKey = null;
  }
  return { ...toControllerView(row), keyThumbprint: row.key_thumbprint, publicKey, replaceCandidate };
}

export type EnrollOutcome =
  | { kind: "row"; controller: ControllerRow }
  | { kind: "pending_limit" }
  | { kind: "pending_locked" };

export type ResolveOutcome =
  | { kind: "activated"; controller: ControllerRow; revoked: ControllerRow | null }
  | { kind: "rejected"; controller: ControllerRow }
  | { kind: "not_found" | "not_pending" | "expired" | "replace_target_invalid" | "cap_reached" };

export type RevokeOutcome = { kind: "revoked" | "already_revoked"; controller: ControllerRow } | { kind: "not_found" };

const COLUMNS = `id, user_id, host_device_id, key_thumbprint, public_key, ordinal, name, name_source, ua_class,
  state, claims_id, requester_session_hash, created_at, last_seen_at, revoked_at, expires_at`;

// Every state change goes through guarded statements here: `revoked` is
// terminal, so nothing in this file ever sets a revoked row back to another
// state or clears its revoked_at. Rows are never deleted, which is what keeps
// max(ordinal)+1 from handing out a retired ordinal.
export class RemoteControllerRepo {
  constructor(private readonly db: D1Database) {}

  async get(userId: number, hostDeviceId: string, id: string): Promise<ControllerRow | null> {
    return this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers WHERE id = ?1 AND user_id = ?2 AND host_device_id = ?3`,
    ).bind(id, userId, hostDeviceId).first<ControllerRow>();
  }

  // The one read a later grant path should use: pending and revoked rows are
  // invisible to it.
  async activeById(userId: number, hostDeviceId: string, id: string): Promise<ControllerRow | null> {
    return this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers
       WHERE id = ?1 AND user_id = ?2 AND host_device_id = ?3 AND state = 'active'`,
    ).bind(id, userId, hostDeviceId).first<ControllerRow>();
  }

  async expirePending(userId: number, hostDeviceId: string, now: Date): Promise<void> {
    const at = now.toISOString();
    await this.db.prepare(
      `UPDATE remote_controllers SET state = 'revoked', revoked_at = ?3
       WHERE user_id = ?1 AND host_device_id = ?2 AND state = 'pending' AND expires_at <= ?3`,
    ).bind(userId, hostDeviceId, at).run();
  }

  async list(userId: number, hostDeviceId: string, states: ControllerState[], now = new Date()): Promise<ControllerRow[]> {
    await this.expirePending(userId, hostDeviceId, now);
    const result = await this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers
       WHERE user_id = ?1 AND host_device_id = ?2
       ORDER BY (state = 'revoked'), COALESCE(ordinal, 1000000000), created_at`,
    ).bind(userId, hostDeviceId).all<ControllerRow>();
    return result.results.filter((row) => states.includes(row.state));
  }

  async leastRecentlyUsed(userId: number, hostDeviceId: string): Promise<ControllerRow | null> {
    return this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers
       WHERE user_id = ?1 AND host_device_id = ?2 AND state = 'active'
       ORDER BY last_seen_at ASC, ordinal ASC LIMIT 1`,
    ).bind(userId, hostDeviceId).first<ControllerRow>();
  }

  // One statement decides the new row's state, so two tabs cannot both become
  // the first device. A host that has never activated a controller auto-enrolls
  // the first one (a revoked device still counts, so revoking the only phone
  // does not reopen it); any further key is held as pending. An existing row for the same key
  // is returned as it stands (a no-op update that only bumps last_seen_at of a
  // row that is not revoked), never rewritten.
  async enroll(input: {
    userId: number;
    hostDeviceId: string;
    id: string;
    thumbprint: string;
    publicKey: ControllerJwk;
    uaClass: ControllerUaClass;
    claimsId: string | null;
    sessionHash: string;
    pendingLocked: boolean;
    now?: Date;
  }): Promise<EnrollOutcome> {
    const now = input.now ?? new Date();
    await this.expirePending(input.userId, input.hostDeviceId, now);
    const at = now.toISOString();
    const row = await this.db.prepare(
      `INSERT INTO remote_controllers (${COLUMNS})
       SELECT ?1, ?2, ?3, ?4, ?5,
         CASE WHEN seen.n > 0 THEN NULL ELSE
           (SELECT COALESCE(MAX(o.ordinal), 0) + 1 FROM remote_controllers o WHERE o.user_id = ?2 AND o.host_device_id = ?3)
         END,
         ?6, 'auto', ?7,
         CASE WHEN seen.n > 0 THEN 'pending' ELSE 'active' END,
         CASE WHEN seen.n > 0 THEN ?8 ELSE NULL END,
         ?9, ?10, ?10, NULL,
         CASE WHEN seen.n > 0 THEN ?11 ELSE NULL END
       FROM (SELECT COUNT(*) AS n FROM remote_controllers a
             WHERE a.user_id = ?2 AND a.host_device_id = ?3 AND a.ordinal IS NOT NULL) AS seen,
            (SELECT COUNT(*) AS n FROM remote_controllers p
             WHERE p.user_id = ?2 AND p.host_device_id = ?3 AND p.state = 'pending' AND p.expires_at > ?10) AS pending
       WHERE seen.n = 0 OR (pending.n < ?12 AND ?13 = 0)
       ON CONFLICT (user_id, host_device_id, key_thumbprint) DO UPDATE SET
         last_seen_at = CASE WHEN remote_controllers.state = 'revoked'
                             THEN remote_controllers.last_seen_at ELSE excluded.last_seen_at END
       RETURNING ${COLUMNS}`,
    ).bind(
      input.id, input.userId, input.hostDeviceId, input.thumbprint,
      JSON.stringify({ kty: input.publicKey.kty, crv: input.publicKey.crv, x: input.publicKey.x, y: input.publicKey.y }),
      CONTROLLER_UA_NAMES[input.uaClass], input.uaClass, input.claimsId, input.sessionHash, at,
      new Date(now.getTime() + REMOTE_PENDING_TTL_MS).toISOString(),
      REMOTE_CONTROLLER_PENDING_CAP, input.pendingLocked ? 1 : 0,
    ).first<ControllerRow>();
    if (row) return { kind: "row", controller: row };
    const existing = await this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers
       WHERE user_id = ?1 AND host_device_id = ?2 AND key_thumbprint = ?3`,
    ).bind(input.userId, input.hostDeviceId, input.thumbprint).first<ControllerRow>();
    if (existing) return { kind: "row", controller: existing };
    return { kind: input.pendingLocked ? "pending_locked" : "pending_limit" };
  }

  async resolve(input: {
    userId: number;
    hostDeviceId: string;
    id: string;
    action: "add" | "replace" | "reject";
    replaceId?: string;
    now?: Date;
  }): Promise<ResolveOutcome> {
    const now = input.now ?? new Date();
    const at = now.toISOString();
    const pending = await this.get(input.userId, input.hostDeviceId, input.id);
    if (!pending) return { kind: "not_found" };
    if (pending.state !== "pending") return { kind: "not_pending" };
    if (!pending.expires_at || pending.expires_at <= at) {
      await this.expirePending(input.userId, input.hostDeviceId, now);
      return { kind: "expired" };
    }

    if (input.action === "reject") {
      const rejected = await this.db.prepare(
        `UPDATE remote_controllers SET state = 'revoked', revoked_at = ?4
         WHERE id = ?1 AND user_id = ?2 AND host_device_id = ?3 AND state = 'pending'
         RETURNING ${COLUMNS}`,
      ).bind(input.id, input.userId, input.hostDeviceId, at).first<ControllerRow>();
      return rejected ? { kind: "rejected", controller: rejected } : { kind: "not_pending" };
    }

    let targetId = "";
    if (input.action === "replace") {
      targetId = input.replaceId ?? "";
      if (!targetId) return { kind: "replace_target_invalid" };
    }
    const idleBefore = new Date(now.getTime() - REMOTE_CONTROLLER_IDLE_MS).toISOString();
    const activate = this.db.prepare(
      `UPDATE remote_controllers SET
         state = 'active',
         ordinal = (SELECT COALESCE(MAX(o.ordinal), 0) + 1 FROM remote_controllers o WHERE o.user_id = ?1 AND o.host_device_id = ?2),
         expires_at = NULL, last_seen_at = ?4
       WHERE id = ?3 AND user_id = ?1 AND host_device_id = ?2 AND state = 'pending' AND expires_at > ?4
         AND (?5 = '' OR EXISTS (SELECT 1 FROM remote_controllers t
              WHERE t.id = ?5 AND t.user_id = ?1 AND t.host_device_id = ?2 AND t.state = 'active'))
         AND (SELECT COUNT(*) FROM remote_controllers a
              WHERE a.user_id = ?1 AND a.host_device_id = ?2 AND a.state = 'active'
                AND a.last_seen_at > ?6 AND a.id != ?5) < ?7
       RETURNING ${COLUMNS}`,
    ).bind(input.userId, input.hostDeviceId, input.id, at, targetId, idleBefore, REMOTE_CONTROLLER_ACTIVE_CAP);
    const statements = [activate];
    if (targetId) {
      statements.push(this.db.prepare(
        `UPDATE remote_controllers SET state = 'revoked', revoked_at = ?4
         WHERE id = ?5 AND user_id = ?1 AND host_device_id = ?2 AND state = 'active'
           AND EXISTS (SELECT 1 FROM remote_controllers n
                       WHERE n.id = ?3 AND n.user_id = ?1 AND n.host_device_id = ?2
                         AND n.state = 'active' AND n.last_seen_at = ?4)
         RETURNING ${COLUMNS}`,
      ).bind(input.userId, input.hostDeviceId, input.id, at, targetId));
    }
    const results = await this.db.batch<ControllerRow>(statements);
    const activated = results[0]?.results?.[0];
    if (activated) return { kind: "activated", controller: activated, revoked: results[1]?.results?.[0] ?? null };

    const current = await this.get(input.userId, input.hostDeviceId, input.id);
    if (!current || current.state !== "pending") return { kind: "not_pending" };
    if (!current.expires_at || current.expires_at <= at) return { kind: "expired" };
    if (targetId && !(await this.activeById(input.userId, input.hostDeviceId, targetId))) {
      return { kind: "replace_target_invalid" };
    }
    return { kind: "cap_reached" };
  }

  // The claimed device when it is still active, otherwise the least recently
  // used one: what a desktop is offered to replace.
  async replaceCandidate(row: ControllerRow): Promise<ControllerRow | null> {
    if (row.claims_id) {
      const claimed = await this.activeById(row.user_id, row.host_device_id, row.claims_id);
      if (claimed) return claimed;
    }
    return this.leastRecentlyUsed(row.user_id, row.host_device_id);
  }

  async revoke(userId: number, id: string, hostDeviceId?: string, now = new Date()): Promise<RevokeOutcome> {
    const at = now.toISOString();
    const revoked = await this.db.prepare(
      `UPDATE remote_controllers SET state = 'revoked', revoked_at = ?3
       WHERE id = ?1 AND user_id = ?2 AND (?4 = '' OR host_device_id = ?4) AND state != 'revoked'
       RETURNING ${COLUMNS}`,
    ).bind(id, userId, at, hostDeviceId ?? "").first<ControllerRow>();
    if (revoked) return { kind: "revoked", controller: revoked };
    const existing = await this.db.prepare(
      `SELECT ${COLUMNS} FROM remote_controllers WHERE id = ?1 AND user_id = ?2 AND (?3 = '' OR host_device_id = ?3)`,
    ).bind(id, userId, hostDeviceId ?? "").first<ControllerRow>();
    return existing ? { kind: "already_revoked", controller: existing } : { kind: "not_found" };
  }
}
