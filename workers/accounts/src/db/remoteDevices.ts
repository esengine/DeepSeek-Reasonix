import { generateToken, hashToken } from "../auth/crypto";

export const REMOTE_CAPABILITIES = ["terminal", "tasks", "logs", "files", "desktop"] as const;
export type RemoteCapability = (typeof REMOTE_CAPABILITIES)[number];

interface RemoteDeviceRow {
  id: string;
  user_id: number;
  name: string;
  platform: string;
  public_key: string;
  capabilities: string;
  created_at: string;
  updated_at: string;
  last_seen_at: string | null;
  revoked_at: string | null;
}

export interface RemoteDevice {
  id: string;
  name: string;
  platform: string;
  publicKey: string;
  capabilities: RemoteCapability[];
  createdAt: string;
  updatedAt: string;
  lastSeenAt: string | null;
  revokedAt: string | null;
}

export interface ConsumedRemoteGrant {
  userId: number;
  targetDeviceId: string;
  scopes: RemoteCapability[];
  sessionId: string | null;
  authenticatedAt: string | null;
}

export interface RemoteLease {
  userId: number;
  deviceId: string;
  sessionId?: string;
}

export type RemoteLeaseVerdict = "active" | "revoked" | "reauth_required";

function parseCapabilities(value: string): RemoteCapability[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  return parsed.filter((item): item is RemoteCapability =>
    typeof item === "string" && (REMOTE_CAPABILITIES as readonly string[]).includes(item));
}

function toDevice(row: RemoteDeviceRow): RemoteDevice {
  return {
    id: row.id,
    name: row.name,
    platform: row.platform,
    publicKey: row.public_key,
    capabilities: parseCapabilities(row.capabilities),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    lastSeenAt: row.last_seen_at,
    revokedAt: row.revoked_at,
  };
}

export class RemoteDeviceRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  async register(input: {
    userId: number;
    name: string;
    platform: string;
    publicKey: string;
    capabilities: RemoteCapability[];
  }): Promise<{ device: RemoteDevice; deviceCredential: string }> {
    const id = generateToken();
    const deviceCredential = generateToken();
    const credentialHash = await hashToken(this.pepper, deviceCredential);
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `INSERT INTO remote_devices (
         id, user_id, credential_hash, name, platform, public_key, capabilities,
         created_at, updated_at, revoked_at
       ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?8, NULL)
       ON CONFLICT (user_id, public_key) DO UPDATE SET
         credential_hash = excluded.credential_hash,
         name = excluded.name,
         platform = excluded.platform,
         capabilities = excluded.capabilities,
         updated_at = excluded.updated_at,
         revoked_at = NULL
       RETURNING id, user_id, name, platform, public_key, capabilities,
                 created_at, updated_at, last_seen_at, revoked_at`,
    ).bind(
      id, input.userId, credentialHash, input.name, input.platform,
      input.publicKey, JSON.stringify(input.capabilities), now,
    ).first<RemoteDeviceRow>();
    if (!row) throw new Error("remote device registration returned no row");
    return { device: toDevice(row), deviceCredential };
  }

  async listForUser(userId: number): Promise<RemoteDevice[]> {
    const result = await this.db.prepare(
      `SELECT id, user_id, name, platform, public_key, capabilities,
              created_at, updated_at, last_seen_at, revoked_at
       FROM remote_devices WHERE user_id = ?1
       ORDER BY revoked_at IS NULL DESC, updated_at DESC`,
    ).bind(userId).all<RemoteDeviceRow>();
    return result.results.map(toDevice);
  }

  async activeForUser(userId: number, deviceId: string): Promise<RemoteDevice | null> {
    const row = await this.db.prepare(
      `SELECT id, user_id, name, platform, public_key, capabilities,
              created_at, updated_at, last_seen_at, revoked_at
       FROM remote_devices WHERE id = ?1 AND user_id = ?2 AND revoked_at IS NULL`,
    ).bind(deviceId, userId).first<RemoteDeviceRow>();
    return row ? toDevice(row) : null;
  }

  async rename(userId: number, deviceId: string, name: string): Promise<RemoteDevice | null> {
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `UPDATE remote_devices SET name = ?1, updated_at = ?2
       WHERE id = ?3 AND user_id = ?4 AND revoked_at IS NULL
       RETURNING id, user_id, name, platform, public_key, capabilities,
                 created_at, updated_at, last_seen_at, revoked_at`,
    ).bind(name, now, deviceId, userId).first<RemoteDeviceRow>();
    return row ? toDevice(row) : null;
  }

  async activeIdsForUser(userId: number): Promise<string[]> {
    const result = await this.db.prepare(
      "SELECT id FROM remote_devices WHERE user_id = ?1 AND revoked_at IS NULL",
    ).bind(userId).all<{ id: string }>();
    return result.results.map((row) => row.id);
  }

  async revoke(userId: number, deviceId: string): Promise<boolean> {
    const now = new Date().toISOString();
    const result = await this.db.prepare(
      `UPDATE remote_devices SET revoked_at = ?1, updated_at = ?1
       WHERE id = ?2 AND user_id = ?3 AND revoked_at IS NULL`,
    ).bind(now, deviceId, userId).run();
    return (result.meta.changes ?? 0) > 0;
  }

  // One batch: host registrations, their pending grants and every controller
  // enrollment of the user (pending included) change together, so a failure
  // cannot leave phones enrolled against revoked computers or the reverse.
  async revokeAllForUser(userId: number): Promise<string[]> {
    const now = new Date().toISOString();
    const [revoked] = await this.db.batch<{ id: string }>([
      this.db.prepare(
        `UPDATE remote_devices SET revoked_at = ?1, updated_at = ?1
         WHERE user_id = ?2 AND revoked_at IS NULL
         RETURNING id`,
      ).bind(now, userId),
      this.db.prepare("DELETE FROM remote_connection_grants WHERE user_id = ?1").bind(userId),
      this.db.prepare(
        `UPDATE remote_controllers SET state = 'revoked', revoked_at = ?1
         WHERE user_id = ?2 AND state != 'revoked'`,
      ).bind(now, userId),
    ]);
    return (revoked?.results ?? []).map((row) => row.id);
  }

  async authenticate(deviceId: string, credential: string): Promise<{ userId: number; device: RemoteDevice } | null> {
    const credentialHash = await hashToken(this.pepper, credential);
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `UPDATE remote_devices SET last_seen_at = ?1, updated_at = ?1
       WHERE id = ?2 AND credential_hash = ?3 AND revoked_at IS NULL
       RETURNING id, user_id, name, platform, public_key, capabilities,
                 created_at, updated_at, last_seen_at, revoked_at`,
    ).bind(now, deviceId, credentialHash).first<RemoteDeviceRow>();
    return row ? { userId: row.user_id, device: toDevice(row) } : null;
  }

  async issueGrant(input: {
    userId: number;
    targetDeviceId: string;
    scopes: RemoteCapability[];
    ttlMs: number;
    session: { id: string; createdAt: string };
  }): Promise<{ ticket: string; expiresAt: string }> {
    const ticket = generateToken();
    const ticketHash = await hashToken(this.pepper, ticket);
    const now = new Date();
    const expiresAt = new Date(now.getTime() + input.ttlMs).toISOString();
    await this.db.prepare(
      `INSERT INTO remote_connection_grants (
         ticket_hash, user_id, target_device_id, scopes, created_at, expires_at,
         session_hash, authenticated_at
       ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
    ).bind(
      ticketHash, input.userId, input.targetDeviceId, JSON.stringify(input.scopes),
      now.toISOString(), expiresAt, input.session.id, input.session.createdAt,
    ).run();
    return { ticket, expiresAt };
  }

  async consumeGrant(ticket: string): Promise<ConsumedRemoteGrant | null> {
    const ticketHash = await hashToken(this.pepper, ticket);
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `DELETE FROM remote_connection_grants
       WHERE ticket_hash = ?1 AND expires_at > ?2
         AND EXISTS (
           SELECT 1 FROM remote_devices d
           WHERE d.id = remote_connection_grants.target_device_id
             AND d.user_id = remote_connection_grants.user_id
             AND d.revoked_at IS NULL
         )
         AND (session_hash IS NULL OR EXISTS (
           SELECT 1 FROM sessions s
           WHERE s.token_hash = remote_connection_grants.session_hash
             AND s.user_id = remote_connection_grants.user_id
             AND s.expires_at > ?2
         ))
       RETURNING user_id, target_device_id, scopes, session_hash, authenticated_at`,
    ).bind(ticketHash, now).first<{
      user_id: number;
      target_device_id: string;
      scopes: string;
      session_hash: string | null;
      authenticated_at: string | null;
    }>();
    if (!row) return null;
    return {
      userId: row.user_id,
      targetDeviceId: row.target_device_id,
      scopes: parseCapabilities(row.scopes),
      sessionId: row.session_hash ?? null,
      authenticatedAt: row.authenticated_at ?? null,
    };
  }

  // Answers whether each live relay connection may continue. A connection that
  // names a session also needs that session to exist and to have signed in
  // after `signedInAfter`; one that names none is held only to its device.
  async checkLeases(leases: RemoteLease[], signedInAfter: string): Promise<RemoteLeaseVerdict[]> {
    const now = new Date().toISOString();
    const verdicts: RemoteLeaseVerdict[] = [];
    for (const lease of leases) {
      const device = await this.db.prepare(
        "SELECT 1 AS ok FROM remote_devices WHERE id = ?1 AND user_id = ?2 AND revoked_at IS NULL",
      ).bind(lease.deviceId, lease.userId).first<{ ok: number }>();
      if (!device) {
        verdicts.push("revoked");
        continue;
      }
      if (!lease.sessionId) {
        verdicts.push("active");
        continue;
      }
      const session = await this.db.prepare(
        `SELECT s.created_at FROM sessions s JOIN users u ON u.id = s.user_id
         WHERE s.token_hash = ?1 AND s.user_id = ?2 AND s.expires_at > ?3
           AND s.kind = 'web' AND u.status = 'active'`,
      ).bind(lease.sessionId, lease.userId, now).first<{ created_at: string }>();
      verdicts.push(session && session.created_at > signedInAfter ? "active" : "reauth_required");
    }
    return verdicts;
  }
}
