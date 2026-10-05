import type { Bindings } from "./env";

const EXPIRING_TABLES = [
  "device_grants",
  "email_tokens",
  "sessions",
  "remote_connection_grants",
  "remote_attachment_grants",
  "remote_controller_challenges",
  "remote_rate_counters",
] as const;

export async function purgeExpiredAuthState(env: Pick<Bindings, "DB">, now = new Date()): Promise<number> {
  const cutoff = now.toISOString();
  const results = await env.DB.batch([
    ...EXPIRING_TABLES.map((table) => env.DB.prepare(`DELETE FROM ${table} WHERE expires_at <= ?1`).bind(cutoff)),
    // A pending controller request that was never answered is revoked, not
    // deleted: the row is the record that the key asked and was refused.
    env.DB.prepare(
      "UPDATE remote_controllers SET state = 'revoked', revoked_at = ?1 WHERE state = 'pending' AND expires_at <= ?1",
    ).bind(cutoff),
  ]);
  return results.reduce((total, result) => total + (result.meta.changes ?? 0), 0);
}
