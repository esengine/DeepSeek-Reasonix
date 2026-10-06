import type { Env } from "./env";
import { accountLinkEnabled } from "./feedback_account";

// The one rule for "this report is the caller's": its own install, or, while the
// link switch is on, a linked sibling install that is not blocked. A block follows the
// install that filed the report, so linking never lends a blocked install a voice.
export function ownedByCaller(env: Env, installHash: string, now: Date): { sql: string; binds: string[] } {
  if (!accountLinkEnabled(env)) return { sql: "install_hash = ?", binds: [installHash] };
  return {
    sql: `(install_hash = ? OR (install_hash IN (SELECT l.install_hash FROM feedback_account_links l
      WHERE l.account_hash = (SELECT account_hash FROM feedback_account_links WHERE install_hash = ?))
      AND 'install:' || install_hash NOT IN (SELECT target FROM feedback_blocks WHERE expires_at IS NULL OR expires_at > ?)))`,
    binds: [installHash, installHash, now.toISOString()],
  };
}
