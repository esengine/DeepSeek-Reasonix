export interface FeishuAccessConfig {
  ownerOpenId?: string;
  allowlist?: readonly string[];
}

export type FeishuAccessMode = "owner" | "allowlist";

export type FeishuAccessDecision =
  | {
      accept: true;
      mode: FeishuAccessMode;
      bindRuntime: boolean;
    }
  | {
      accept: false;
      reason: "unauthorized";
    };

export function normalizeFeishuOpenId(value: string | null | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

export function normalizeFeishuAllowlist(
  values: readonly string[] | string | null | undefined,
): string[] | undefined {
  const list =
    typeof values === "string" ? values.split(/[,\s]+/) : Array.isArray(values) ? values : [];
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const raw of list) {
    const openid = normalizeFeishuOpenId(raw);
    if (!openid || seen.has(openid)) continue;
    seen.add(openid);
    normalized.push(openid);
  }
  return normalized.length > 0 ? normalized : undefined;
}

export function redactFeishuOpenId(openid: string | null | undefined): string {
  const normalized = normalizeFeishuOpenId(openid);
  if (!normalized) return "none";
  if (normalized.length <= 10) return normalized;
  return `${normalized.slice(0, 6)}...${normalized.slice(-4)}`;
}

export function decideFeishuAccess(
  config: FeishuAccessConfig,
  openid: string,
): FeishuAccessDecision {
  const candidate = normalizeFeishuOpenId(openid);
  if (!candidate) return { accept: false, reason: "unauthorized" };

  const ownerOpenId = normalizeFeishuOpenId(config.ownerOpenId);
  const allowlist = normalizeFeishuAllowlist(config.allowlist) ?? [];

  if (ownerOpenId && candidate === ownerOpenId) {
    return { accept: true, mode: "owner", bindRuntime: false };
  }
  if (allowlist.includes(candidate)) {
    return { accept: true, mode: "allowlist", bindRuntime: false };
  }
  if (ownerOpenId || allowlist.length > 0) {
    return { accept: false, reason: "unauthorized" };
  }
  return { accept: false, reason: "unauthorized" };
}

export function describeFeishuAccess(config: FeishuAccessConfig): string {
  const ownerOpenId = normalizeFeishuOpenId(config.ownerOpenId);
  const allowlist = normalizeFeishuAllowlist(config.allowlist) ?? [];

  if (ownerOpenId) {
    const suffix = allowlist.length > 0 ? `, allowlist ${allowlist.length}` : "";
    return `owner ${redactFeishuOpenId(ownerOpenId)}${suffix}`;
  }
  if (allowlist.length > 0) {
    return `allowlist ${allowlist.length}`;
  }
  return "restricted (unconfigured)";
}
