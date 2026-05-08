import type { DeepSeekClient } from "../client.js";
import { t } from "../i18n/index.js";

export interface DeepSeekProbeResult {
  reachable: boolean;
}

export function formatLoopError(err: Error, probe?: DeepSeekProbeResult): string {
  const msg = err.message ?? "";
  if (msg.includes("maximum context length")) {
    const reqMatch = msg.match(/requested\s+(\d+)\s+tokens/);
    const requested = reqMatch
      ? `${Number(reqMatch[1]).toLocaleString()} tokens`
      : t("errors.contextOverflowTooMany");
    return t("errors.contextOverflow", { requested });
  }

  const m = /^DeepSeek (\d{3}):\s*([\s\S]*)$/.exec(msg);
  if (!m) return msg;
  const status = m[1] ?? "";
  const body = m[2] ?? "";
  const inner = extractDeepSeekErrorMessage(body);

  if (status === "401") return t("errors.auth401", { inner });
  if (status === "402") return t("errors.balance402", { inner });
  if (status === "422") return t("errors.badparam422", { inner });
  if (status === "400") return t("errors.badrequest400", { inner });
  if (is5xxStatus(status)) return formatDeepSeek5xx(status, probe);
  return msg;
}

export function is5xxError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const m = /^DeepSeek (5\d{2}):/.exec(err.message ?? "");
  return m !== null;
}

export async function probeDeepSeekReachable(
  client: DeepSeekClient,
  timeoutMs = 1500,
): Promise<DeepSeekProbeResult> {
  const balance = await client.getBalance({ signal: AbortSignal.timeout(timeoutMs) });
  return { reachable: balance !== null };
}

function is5xxStatus(status: string): boolean {
  return status === "500" || status === "502" || status === "503" || status === "504";
}

function formatDeepSeek5xx(status: string, probe?: DeepSeekProbeResult): string {
  const head = t("errors.deepseek5xxHead", { status });
  const probeNote =
    probe === undefined
      ? ""
      : probe.reachable
        ? t("errors.deepseek5xxReachable")
        : t("errors.deepseek5xxUnreachable");
  const action =
    probe?.reachable === false
      ? t("errors.deepseek5xxActionNetwork")
      : t("errors.deepseek5xxActionRetry");
  return `${head}${probeNote}${action}`;
}

export function reasonPrefixFor(
  reason: "budget" | "aborted" | "context-guard" | "stuck",
  iterCap: number,
): string {
  if (reason === "aborted") return t("errors.reasonAborted");
  if (reason === "context-guard") return t("errors.reasonContextGuard");
  if (reason === "stuck") return t("errors.reasonStuck");
  return t("errors.reasonBudget", { iterCap });
}

export function errorLabelFor(
  reason: "budget" | "aborted" | "context-guard" | "stuck",
  iterCap: number,
): string {
  if (reason === "aborted") return t("errors.labelAborted");
  if (reason === "context-guard") return t("errors.labelContextGuard");
  if (reason === "stuck") return t("errors.labelStuck");
  return t("errors.labelBudget", { iterCap });
}

function extractDeepSeekErrorMessage(body: string): string {
  const trimmed = body.trim();
  if (!trimmed) return t("errors.innerNoMessage");
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object") {
      const obj = parsed as { error?: { message?: unknown }; message?: unknown };
      if (obj.error && typeof obj.error.message === "string") return obj.error.message;
      if (typeof obj.message === "string") return obj.message;
    }
  } catch {
    /* not JSON — fall through */
  }
  return trimmed;
}
