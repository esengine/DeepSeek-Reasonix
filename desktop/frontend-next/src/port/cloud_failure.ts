import { t } from "../i18n";

// Why a relay connection could not be made or kept. A browser is told nothing
// about a refused WebSocket upgrade, so these come from what can be observed
// around it, never from the text of an error.
export type RelayFailure =
  | "offline"
  | "unreachable"
  | "origin_rejected"
  | "rate_limited"
  | "refused"
  | "idle"
  | "unknown";

export interface FailureDetail {
  code: RelayFailure;
  retryAfterS?: number;
}

export type FailureAction = "reconnect" | "signin" | "home";

export interface FailureView {
  title: string;
  body: string;
  action: FailureAction;
  waitS: number;
  retryWhenOnline: boolean;
}

export function failureView({ code, retryAfterS }: FailureDetail): FailureView {
  const reconnect = { action: "reconnect" as const, waitS: 0, retryWhenOnline: false };
  switch (code) {
    case "offline":
      return { ...reconnect, retryWhenOnline: true, title: t("remote.youre_offline"), body: t("remote.this_device_has_no_network") };
    case "unreachable":
      return {
        ...reconnect,
        title: t("remote.cant_reach_the_remote_service"),
        body: t("remote.this_network_cant_reach_the"),
      };
    case "origin_rejected":
      return {
        ...reconnect,
        action: "home",
        title: t("remote.this_address_cant_use_the"),
        body: t("remote.the_relay_rejected_this_pages"),
      };
    case "rate_limited": {
      const waitS = retryAfterS && retryAfterS > 0 ? Math.min(Math.ceil(retryAfterS), 600) : 0;
      return {
        action: "reconnect",
        retryWhenOnline: false,
        waitS,
        title: t("remote.too_many_attempts"),
        body: waitS > 0 ? t("remote.too_many_requests_wait_n", { n: waitS }) : t("remote.too_many_requests_wait_a"),
      };
    }
    case "refused":
      return {
        ...reconnect,
        title: t("remote.the_relay_refused_this_connection"),
        body: t("remote.the_connection_ticket_may_have"),
      };
    case "idle":
      return {
        ...reconnect,
        title: t("remote.inactive_for_a_while"),
        body: t("remote.the_relay_released_this_idle"),
      };
    case "unknown":
      return { ...reconnect, title: t("remote.remote_connection_failed"), body: t("remote.the_remote_relay_is_temporarily") };
  }
}

export interface ProbeOptions {
  relay: string;
  online: () => boolean;
  fetch: typeof fetch;
  timeoutMs?: number;
}

const PROBE_TIMEOUT_MS = 5_000;

interface Refusal {
  error?: { code?: string };
}

// The gateway's /v1/probe answers a CORS reader whether or not its origin is
// allowed, so a status the browser can read is the gateway speaking. When
// nothing readable comes back, the health route (which carries no CORS
// headers) can only show that something answered, and that is "unknown", not
// a diagnosis.
export async function classifyHandshakeFailure(options: ProbeOptions): Promise<FailureDetail> {
  if (!options.online()) return { code: "offline" };
  const timeout = options.timeoutMs ?? PROBE_TIMEOUT_MS;
  const base = options.relay.replace(/^ws/, "http");
  let answer: Response;
  try {
    answer = await options.fetch(`${base}/v1/probe`, { mode: "cors", cache: "no-store", signal: AbortSignal.timeout(timeout) });
  } catch {
    if (!options.online()) return { code: "offline" };
    try {
      await options.fetch(`${base}/health`, { mode: "no-cors", cache: "no-store", signal: AbortSignal.timeout(timeout) });
    } catch {
      return { code: options.online() ? "unreachable" : "offline" };
    }
    return { code: "unknown" };
  }
  if (answer.ok) return { code: "refused" };
  if (answer.status === 403) {
    const body = await answer.json().catch(() => null) as Refusal | null;
    if (body?.error?.code === "origin_rejected") return { code: "origin_rejected" };
    return { code: "unknown" };
  }
  if (answer.status === 429) return { code: "rate_limited", retryAfterS: retryAfterSeconds(answer.headers.get("retry-after")) };
  return { code: answer.status >= 500 ? "unreachable" : "unknown" };
}

export function retryAfterSeconds(value: string | null): number | undefined {
  if (value === null) return undefined;
  const seconds = Number(value.trim());
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}
