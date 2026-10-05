import { generateToken, hashToken } from "../auth/crypto";

export type ChallengePurpose = "enroll";

export interface ChallengeBinding {
  userId: number;
  hostDeviceId: string;
  sessionHash: string;
  purpose: ChallengePurpose;
}

export type ChallengeConsumption = "consumed" | "expired" | "invalid";

export class RemoteChallengeRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  async issue(binding: ChallengeBinding, ttlMs: number): Promise<{ nonce: string; expiresAt: string }> {
    const nonce = generateToken();
    const now = new Date();
    const expiresAt = new Date(now.getTime() + ttlMs).toISOString();
    await this.db.prepare(
      `INSERT INTO remote_controller_challenges (nonce_hash, user_id, host_device_id, session_hash, purpose, created_at, expires_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
    ).bind(
      await hashToken(this.pepper, nonce), binding.userId, binding.hostDeviceId, binding.sessionHash,
      binding.purpose, now.toISOString(), expiresAt,
    ).run();
    return { nonce, expiresAt };
  }

  // Deletes the nonce only when every binding matches, so a caller with the
  // wrong user, host, session or purpose cannot burn someone else's challenge.
  // The caller verifies the signature afterwards: a failed proof has already
  // spent the nonce.
  async consume(nonce: string, binding: ChallengeBinding): Promise<ChallengeConsumption> {
    const nonceHash = await hashToken(this.pepper, nonce);
    const now = new Date().toISOString();
    const taken = await this.db.prepare(
      `DELETE FROM remote_controller_challenges
       WHERE nonce_hash = ?1 AND user_id = ?2 AND host_device_id = ?3 AND session_hash = ?4
         AND purpose = ?5 AND expires_at > ?6
       RETURNING nonce_hash`,
    ).bind(nonceHash, binding.userId, binding.hostDeviceId, binding.sessionHash, binding.purpose, now)
      .first<{ nonce_hash: string }>();
    if (taken) return "consumed";
    const stale = await this.db.prepare(
      `DELETE FROM remote_controller_challenges
       WHERE nonce_hash = ?1 AND user_id = ?2 AND host_device_id = ?3 AND session_hash = ?4
         AND purpose = ?5 AND expires_at <= ?6
       RETURNING nonce_hash`,
    ).bind(nonceHash, binding.userId, binding.hostDeviceId, binding.sessionHash, binding.purpose, now)
      .first<{ nonce_hash: string }>();
    return stale ? "expired" : "invalid";
  }
}
