export interface TelegramAccessConfig {
  ownerUserId?: string;
  allowlist?: readonly string[];
  runtimeBoundUserId?: string | null;
}

export type TelegramAccessMode = "owner" | "allowlist" | "runtime";

export type TelegramAccessDecision =
  | {
      accept: true;
      mode: TelegramAccessMode;
      bindRuntime: boolean;
    }
  | {
      accept: false;
      reason: "unauthorized";
    };

export function normalizeTelegramUserId(
  value: string | number | null | undefined,
): string | undefined {
  const trimmed = String(value ?? "").trim();
  return trimmed ? trimmed : undefined;
}

export function normalizeTelegramAllowlist(
  values: readonly string[] | string | null | undefined,
): string[] | undefined {
  const list =
    typeof values === "string" ? values.split(/[,\s]+/) : Array.isArray(values) ? values : [];
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const raw of list) {
    const userId = normalizeTelegramUserId(raw);
    if (!userId || seen.has(userId)) continue;
    seen.add(userId);
    normalized.push(userId);
  }
  return normalized.length > 0 ? normalized : undefined;
}

export function redactTelegramUserId(userId: string | null | undefined): string {
  const normalized = normalizeTelegramUserId(userId);
  if (!normalized) return "none";
  if (normalized.length <= 8) return normalized;
  return `${normalized.slice(0, 4)}...${normalized.slice(-4)}`;
}

export function decideTelegramAccess(
  config: TelegramAccessConfig,
  userId: string,
): TelegramAccessDecision {
  const candidate = normalizeTelegramUserId(userId);
  if (!candidate) return { accept: false, reason: "unauthorized" };

  const ownerUserId = normalizeTelegramUserId(config.ownerUserId);
  const allowlist = normalizeTelegramAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeTelegramUserId(config.runtimeBoundUserId);

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

export function describeTelegramAccess(config: TelegramAccessConfig): string {
  const ownerUserId = normalizeTelegramUserId(config.ownerUserId);
  const allowlist = normalizeTelegramAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeTelegramUserId(config.runtimeBoundUserId);

  if (ownerUserId) {
    const suffix = allowlist.length > 0 ? `, allowlist ${allowlist.length}` : "";
    return `owner ${redactTelegramUserId(ownerUserId)}${suffix}`;
  }
  if (allowlist.length > 0) {
    return `allowlist ${allowlist.length}`;
  }
  if (runtimeBoundUserId) {
    return `first-sender (runtime only, ${redactTelegramUserId(runtimeBoundUserId)})`;
  }
  return "access control required";
}
