import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { atomicWriteSync } from "../core/atomic-write.js";

export interface WeixinAccountCredentials {
  accountId: string;
  token: string;
  baseUrl?: string;
  userId?: string;
  savedAt?: string;
}

export function weixinAccountsDir(): string {
  const override = process.env.REASONIX_WEIXIN_ACCOUNTS_DIR?.trim();
  if (override) return override;
  return join(homedir(), ".reasonix", "weixin", "accounts");
}

export function weixinAccountPath(accountId: string): string {
  return join(weixinAccountsDir(), `${accountId}.json`);
}

export function loadWeixinAccount(accountId: string): WeixinAccountCredentials | null {
  const path = weixinAccountPath(accountId);
  if (!existsSync(path)) return null;
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
    const token = String(parsed.token ?? "").trim();
    if (!token) return null;
    return {
      accountId,
      token,
      baseUrl: String(parsed.base_url ?? parsed.baseUrl ?? "").trim() || undefined,
      userId: String(parsed.user_id ?? parsed.userId ?? "").trim() || undefined,
      savedAt: String(parsed.saved_at ?? parsed.savedAt ?? "").trim() || undefined,
    };
  } catch {
    return null;
  }
}

export function saveWeixinAccount(credentials: WeixinAccountCredentials): void {
  const accountId = credentials.accountId.trim();
  const token = credentials.token.trim();
  if (!accountId || !token) return;
  const path = weixinAccountPath(accountId);
  mkdirSync(dirname(path), { recursive: true });
  atomicWriteSync(
    path,
    JSON.stringify(
      {
        token,
        base_url: credentials.baseUrl,
        user_id: credentials.userId,
        saved_at: credentials.savedAt ?? new Date().toISOString(),
      },
      null,
      2,
    ),
    `${path}.tmp`,
  );
}
