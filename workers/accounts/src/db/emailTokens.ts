import { generateToken, hashToken } from "../auth/crypto";

export type EmailTokenPurpose = "verify" | "reset";

export class EmailTokenRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  // Issues a single-use token and returns the raw value for the email link.
  async issue(userId: number, purpose: EmailTokenPurpose, ttlMs: number): Promise<string> {
    const token = generateToken();
    const tokenHash = await hashToken(this.pepper, token);
    const now = new Date();
    const expires = new Date(now.getTime() + ttlMs);
    await this.db
      .prepare("INSERT INTO email_tokens (token_hash, user_id, purpose, created_at, expires_at) VALUES (?1, ?2, ?3, ?4, ?5)")
      .bind(tokenHash, userId, purpose, now.toISOString(), expires.toISOString())
      .run();
    return token;
  }

  // Atomically redeems a token: a single UPDATE ... RETURNING marks it used and
  // hands back the user id, so a token can never be consumed twice.
  async consume(token: string, purpose: EmailTokenPurpose): Promise<number | null> {
    const tokenHash = await hashToken(this.pepper, token);
    const now = new Date().toISOString();
    const row = await this.db
      .prepare(
        `UPDATE email_tokens SET used_at = ?1
         WHERE token_hash = ?2 AND purpose = ?3 AND used_at IS NULL AND expires_at > ?1
         RETURNING user_id`,
      )
      .bind(now, tokenHash, purpose)
      .first<{ user_id: number }>();
    return row?.user_id ?? null;
  }

  async invalidateForUser(userId: number, purpose: EmailTokenPurpose): Promise<void> {
    await this.db
      .prepare("UPDATE email_tokens SET used_at = ?1 WHERE user_id = ?2 AND purpose = ?3 AND used_at IS NULL")
      .bind(new Date().toISOString(), userId, purpose)
      .run();
  }
}
