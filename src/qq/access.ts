export interface QQAccessConfig {
  ownerOpenId?: string;
  allowlist?: readonly string[];
  runtimeBoundOpenId?: string | null;
}

export type QQAccessMode = "owner" | "allowlist" | "runtime" | "open";

export type QQAccessDecision =
  | {
      accept: true;
      mode: QQAccessMode;
      bindRuntime: boolean;
    }
  | {
      accept: false;
      reason: "unauthorized";
    };

export function normalizeQQOpenId(value: string | null | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

export function normalizeQQAllowlist(
  values: readonly string[] | string | null | undefined,
): string[] | undefined {
  const list =
    typeof values === "string" ? values.split(/[,\s]+/) : Array.isArray(values) ? values : [];
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const raw of list) {
    const openid = normalizeQQOpenId(raw);
    if (!openid || seen.has(openid)) continue;
    seen.add(openid);
    normalized.push(openid);
  }
  return normalized.length > 0 ? normalized : undefined;
}

export function redactQQOpenId(openid: string | null | undefined): string {
  const normalized = normalizeQQOpenId(openid);
  if (!normalized) return "none";
  if (normalized.length <= 10) return normalized;
  return `${normalized.slice(0, 6)}...${normalized.slice(-4)}`;
}

export function decideQQAccess(config: QQAccessConfig, openid: string): QQAccessDecision {
  const candidate = normalizeQQOpenId(openid);
  if (!candidate) return { accept: false, reason: "unauthorized" };

  const ownerOpenId = normalizeQQOpenId(config.ownerOpenId);
  const allowlist = normalizeQQAllowlist(config.allowlist) ?? [];
  const runtimeBoundOpenId = normalizeQQOpenId(config.runtimeBoundOpenId);

  if (ownerOpenId && candidate === ownerOpenId) {
    return { accept: true, mode: "owner", bindRuntime: false };
  }
  if (allowlist.includes(candidate)) {
    return { accept: true, mode: "allowlist", bindRuntime: false };
  }
  if (ownerOpenId || allowlist.length > 0) {
    return { accept: false, reason: "unauthorized" };
  }
  if (runtimeBoundOpenId) {
    if (candidate === runtimeBoundOpenId) {
      return { accept: true, mode: "runtime", bindRuntime: false };
    }
    return { accept: false, reason: "unauthorized" };
  }
  return { accept: true, mode: "open", bindRuntime: true };
}

export function describeQQAccess(config: QQAccessConfig): string {
  const ownerOpenId = normalizeQQOpenId(config.ownerOpenId);
  const allowlist = normalizeQQAllowlist(config.allowlist) ?? [];
  const runtimeBoundOpenId = normalizeQQOpenId(config.runtimeBoundOpenId);

  if (ownerOpenId) {
    const suffix = allowlist.length > 0 ? `, allowlist ${allowlist.length}` : "";
    return `owner ${redactQQOpenId(ownerOpenId)}${suffix}`;
  }
  if (allowlist.length > 0) {
    return `allowlist ${allowlist.length}`;
  }
  if (runtimeBoundOpenId) {
    return `first-sender (runtime only, ${redactQQOpenId(runtimeBoundOpenId)})`;
  }
  return "open (unbound)";
}
