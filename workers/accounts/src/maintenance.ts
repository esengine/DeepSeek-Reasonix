import type { Bindings } from "./env";

const EXPIRING_TABLES = [
  "device_grants",
  "email_tokens",
  "sessions",
  "remote_connection_grants",
  "remote_attachment_grants",
] as const;

export async function purgeExpiredAuthState(env: Pick<Bindings, "DB">, now = new Date()): Promise<number> {
  const cutoff = now.toISOString();
  const results = await env.DB.batch(
    EXPIRING_TABLES.map((table) => env.DB.prepare(`DELETE FROM ${table} WHERE expires_at <= ?1`).bind(cutoff)),
  );
  return results.reduce((total, result) => total + (result.meta.changes ?? 0), 0);
}
