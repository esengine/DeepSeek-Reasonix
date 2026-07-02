import { generateToken, generateUserCode, hashToken } from "../auth/crypto";
import { DEVICE_POLL_INTERVAL_S } from "../config";

export type DeviceGrantStatus = "pending" | "approved" | "denied";
export type SessionKind = "web" | "cli";

export interface StartedGrant {
  deviceCode: string;
  userCode: string;
  expiresAt: string;
}

export interface DeviceGrantInfo {
  userCode: string;
  userAgent: string;
  createdAt: string;
  expiresAt: string;
}

export interface ClaimedGrant {
  userId: number;
  kind: SessionKind;
  userAgent: string;
}

export type PollStatus =
  | { kind: "pending"; slowDown: boolean }
  | { kind: "denied" }
  | { kind: "expired" }
  | { kind: "not_found" };

// Strip separators/whitespace and upper-case so a user code typed as "wdjb-mjht"
// or "WDJB MJHT" matches the canonical form stored at issue time.
export function normalizeUserCode(raw: string): string {
  return raw.replace(/[^0-9a-zA-Z]/g, "").toUpperCase();
}

// Present a canonical code as two hyphen-separated groups for readability.
export function formatUserCode(canonical: string): string {
  const mid = Math.ceil(canonical.length / 2);
  return `${canonical.slice(0, mid)}-${canonical.slice(mid)}`;
}

export class DeviceGrantRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  // Issues a pending grant. Returns the raw device_code (for the client to poll)
  // and the canonical user_code (for the human to approve); only the device
  // code's peppered hash is stored, mirroring sessions/email tokens.
  async start(opts: { userAgent?: string; ttlMs: number; kind?: SessionKind }): Promise<StartedGrant> {
    const deviceCode = generateToken();
    const deviceCodeHash = await hashToken(this.pepper, deviceCode);
    const now = new Date();
    const expiresAt = new Date(now.getTime() + opts.ttlMs).toISOString();
    const userAgent = (opts.userAgent ?? "").slice(0, 256);
    const kind: SessionKind = opts.kind ?? "cli";
    for (let attempt = 0; ; attempt++) {
      const userCode = generateUserCode();
      try {
        await this.db
          .prepare(
            `INSERT INTO device_grants (device_code_hash, user_code, status, kind, user_agent, created_at, expires_at)
             VALUES (?1, ?2, 'pending', ?3, ?4, ?5, ?6)`,
          )
          .bind(deviceCodeHash, userCode, kind, userAgent, now.toISOString(), expiresAt)
          .run();
        return { deviceCode, userCode, expiresAt };
      } catch (err) {
        if (attempt >= 4) throw err; // exhausted user_code collision retries
      }
    }
  }

  // Safe display metadata for the approval screen; only a live pending grant is
  // revealed, so an expired or already-decided code looks unknown.
  async info(userCode: string): Promise<DeviceGrantInfo | null> {
    const now = new Date().toISOString();
    const row = await this.db
      .prepare(
        `SELECT user_code, user_agent, created_at, expires_at FROM device_grants
         WHERE user_code = ?1 AND status = 'pending' AND expires_at > ?2`,
      )
      .bind(normalizeUserCode(userCode), now)
      .first<{ user_code: string; user_agent: string; created_at: string; expires_at: string }>();
    if (!row) return null;
    return { userCode: row.user_code, userAgent: row.user_agent, createdAt: row.created_at, expiresAt: row.expires_at };
  }

  // Binds a pending grant to the approving user. Returns false when the code is
  // unknown, expired, or already decided.
  async approve(userCode: string, userId: number): Promise<boolean> {
    const now = new Date().toISOString();
    const row = await this.db
      .prepare(
        `UPDATE device_grants SET status = 'approved', user_id = ?1, approved_at = ?2
         WHERE user_code = ?3 AND status = 'pending' AND expires_at > ?2
         RETURNING device_code_hash`,
      )
      .bind(userId, now, normalizeUserCode(userCode))
      .first<{ device_code_hash: string }>();
    return row !== null;
  }

  async deny(userCode: string): Promise<void> {
    const now = new Date().toISOString();
    await this.db
      .prepare(
        `UPDATE device_grants SET status = 'denied'
         WHERE user_code = ?1 AND status = 'pending' AND expires_at > ?2`,
      )
      .bind(normalizeUserCode(userCode), now)
      .run();
  }

  // Atomically claims an approved grant via DELETE ... RETURNING: exactly one
  // poll wins and gets the bound user; the row is gone afterwards so a session is
  // minted once. The caller creates the session (this repo never sees the pepper
  // twice). A losing poll gets null and falls through to pollStatus.
  async claim(deviceCode: string): Promise<ClaimedGrant | null> {
    const deviceCodeHash = await hashToken(this.pepper, deviceCode);
    const now = new Date().toISOString();
    const row = await this.db
      .prepare(
        `DELETE FROM device_grants
         WHERE device_code_hash = ?1 AND status = 'approved' AND user_id IS NOT NULL AND expires_at > ?2
         RETURNING user_id, kind, user_agent`,
      )
      .bind(deviceCodeHash, now)
      .first<{ user_id: number; kind: SessionKind; user_agent: string }>();
    if (!row) return null;
    return { userId: row.user_id, kind: row.kind, userAgent: row.user_agent };
  }

  // Reports why a not-yet-claimable poll isn't complete. Prunes an expired grant.
  async pollStatus(deviceCode: string): Promise<PollStatus> {
    const deviceCodeHash = await hashToken(this.pepper, deviceCode);
    const now = new Date();
    const nowIso = now.toISOString();
    const row = await this.db
      .prepare(`SELECT status, expires_at, last_polled_at FROM device_grants WHERE device_code_hash = ?1`)
      .bind(deviceCodeHash)
      .first<{ status: DeviceGrantStatus; expires_at: string; last_polled_at: string | null }>();
    if (!row) return { kind: "not_found" };
    if (row.expires_at <= nowIso) {
      await this.db.prepare("DELETE FROM device_grants WHERE device_code_hash = ?1").bind(deviceCodeHash).run();
      return { kind: "expired" };
    }
    if (row.status === "denied") return { kind: "denied" };
    const slowDown =
      row.last_polled_at !== null && now.getTime() - new Date(row.last_polled_at).getTime() < DEVICE_POLL_INTERVAL_S * 1000;
    await this.db.prepare("UPDATE device_grants SET last_polled_at = ?1 WHERE device_code_hash = ?2").bind(nowIso, deviceCodeHash).run();
    return { kind: "pending", slowDown };
  }
}
