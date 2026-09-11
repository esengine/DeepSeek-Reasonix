import type { ServerView } from "./types";

export function canUseNativeMCPOAuth(s: ServerView): boolean {
  const transport = (s.transport || "").trim().toLowerCase();
  if (s.authConfigured || !["http", "streamable-http", "streamable_http"].includes(transport)) return false;
  try {
    const parsed = new URL((s.url || s.authUrl || "").trim());
    if (parsed.username || parsed.password || parsed.hash) return false;
    for (const key of parsed.searchParams.keys()) {
      if (isAuthQueryKey(key)) return false;
    }
    if (parsed.protocol === "https:") return true;
    if (parsed.protocol !== "http:") return false;
    const host = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, "");
    return host === "localhost" || host === "127.0.0.1" || host === "::1";
  } catch {
    return false;
  }
}

function isAuthQueryKey(key: string): boolean {
  const normalized = key.trim().toLowerCase().replace(/[\-_ ]/g, "");
  if ([
    "auth", "authorization", "bearer", "credential", "credentials", "key", "sig", "signature", "hmac",
    "token", "accesstoken", "idtoken", "refreshtoken", "apikey", "accesskey", "secretkey",
    "subscriptionkey", "clientsecret", "password", "passwd",
  ].includes(normalized)) return true;
  return ["token", "secret", "password", "passwd", "apikey", "signature"].some((suffix) => normalized.endsWith(suffix));
}
