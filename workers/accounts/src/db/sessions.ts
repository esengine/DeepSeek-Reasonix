import type { UserRow } from "../types";
import { generateToken, hashToken } from "../auth/crypto";
import { SESSION_TTL_MS } from "../config";

export interface NewSession {
  kind?: "web" | "cli";
  userAgent?: string;
  ttlMs?: number;
}

export class SessionRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  // Persists sha256(pepper:token) and returns the raw token for the cookie.
  async create(userId: number, opts: NewSession = {}): Promise<string> {
    const token = generateToken();
    const tokenHash = await hashToken(this.pepper, token);
    const now = new Date();
    const expires = new Date(now.getTime() + (opts.ttlMs ?? SESSION_TTL_MS));
    await this.db
      .prepare(
        `INSERT INTO sessions (token_hash, user_id, kind, user_agent, created_at, last_seen_at, expires_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?5, ?6)`,
      )
      .bind(tokenHash, userId, opts.kind ?? "web", (opts.userAgent ?? "").slice(0, 256), now.toISOString(), expires.toISOString())
      .run();
    return token;
  }

  // Resolves a cookie token to its active user, pruning the row if expired.
  async resolve(token: string): Promise<UserRow | null> {
    const tokenHash = await hashToken(this.pepper, token);
    const row = await this.db
      .prepare(
        `SELECT u.*, s.expires_at AS s_expires
         FROM sessions s JOIN users u ON u.id = s.user_id
         WHERE s.token_hash = ?1`,
      )
      .bind(tokenHash)
      .first<UserRow & { s_expires: string }>();
    if (!row) return null;
    if (row.s_expires <= new Date().toISOString()) {
      await this.deleteByHash(tokenHash);
      return null;
    }
    if (row.status !== "active") return null;
    const { s_expires: _ignored, ...user } = row;
    return user;
  }

  async deleteByToken(token: string): Promise<void> {
    await this.deleteByHash(await hashToken(this.pepper, token));
  }

  async deleteAllForUser(userId: number): Promise<void> {
    await this.db.prepare("DELETE FROM sessions WHERE user_id = ?1").bind(userId).run();
  }

  private async deleteByHash(tokenHash: string): Promise<void> {
    await this.db.prepare("DELETE FROM sessions WHERE token_hash = ?1").bind(tokenHash).run();
  }
}
