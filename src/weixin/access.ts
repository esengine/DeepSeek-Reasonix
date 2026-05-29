export interface WeixinAccessConfig {
  ownerUserId?: string;
  allowlist?: readonly string[];
  runtimeBoundUserId?: string | null;
}

export type WeixinAccessMode = "owner" | "allowlist" | "runtime";

export type WeixinAccessDecision =
  | {
      accept: true;
      mode: WeixinAccessMode;
      bindRuntime: boolean;
    }
  | {
      accept: false;
      reason: "unauthorized";
    };

export function normalizeWeixinUserId(value: string | null | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

export function normalizeWeixinAllowlist(
  values: readonly string[] | string | null | undefined,
): string[] | undefined {
  const list =
    typeof values === "string" ? values.split(/[,\s]+/) : Array.isArray(values) ? values : [];
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const raw of list) {
    const userId = normalizeWeixinUserId(raw);
    if (!userId || seen.has(userId)) continue;
    seen.add(userId);
    normalized.push(userId);
  }
  return normalized.length > 0 ? normalized : undefined;
}

export function redactWeixinUserId(userId: string | null | undefined): string {
  const normalized = normalizeWeixinUserId(userId);
  if (!normalized) return "none";
  if (normalized.length <= 10) return normalized;
  return `${normalized.slice(0, 6)}...${normalized.slice(-4)}`;
}

export function decideWeixinAccess(
  config: WeixinAccessConfig,
  userId: string,
): WeixinAccessDecision {
  const candidate = normalizeWeixinUserId(userId);
  if (!candidate) return { accept: false, reason: "unauthorized" };

  const ownerUserId = normalizeWeixinUserId(config.ownerUserId);
  const allowlist = normalizeWeixinAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeWeixinUserId(config.runtimeBoundUserId);

  if (ownerUserId && candidate === ownerUserId) {
    return { accept: true, mode: "owner", bindRuntime: false };
  }
  if (allowlist.includes(candidate)) {
    return { accept: true, mode: "allowlist", bindRuntime: false };
  }
  if (ownerUserId || allowlist.length > 0) {
    return { accept: false, reason: "unauthorized" };
  }
  if (runtimeBoundUserId) {
    if (candidate === runtimeBoundUserId) {
      return { accept: true, mode: "runtime", bindRuntime: false };
    }
    return { accept: false, reason: "unauthorized" };
  }
  return { accept: false, reason: "unauthorized" };
}

export function describeWeixinAccess(config: WeixinAccessConfig): string {
  const ownerUserId = normalizeWeixinUserId(config.ownerUserId);
  const allowlist = normalizeWeixinAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeWeixinUserId(config.runtimeBoundUserId);

  if (ownerUserId) {
    const suffix = allowlist.length > 0 ? `, allowlist ${allowlist.length}` : "";
    return `owner ${redactWeixinUserId(ownerUserId)}${suffix}`;
  }
  if (allowlist.length > 0) {
    return `allowlist ${allowlist.length}`;
  }
  if (runtimeBoundUserId) {
    return `first-sender (runtime only, ${redactWeixinUserId(runtimeBoundUserId)})`;
  }
  return "access control required";
}
