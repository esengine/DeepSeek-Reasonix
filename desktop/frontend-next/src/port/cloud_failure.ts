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
      return { ...reconnect, retryWhenOnline: true, title: t("网络已断开"), body: t("当前设备没有网络。请检查网络连接后重新连接。") };
    case "unreachable":
      return {
        ...reconnect,
        title: t("无法连接远程服务"),
        body: t("当前网络访问不到 Reasonix 远程服务，可能是网络受限或服务暂时中断。请切换网络后重新连接。"),
      };
    case "origin_rejected":
      return {
        ...reconnect,
        action: "home",
        title: t("此网址不能使用远程中转"),
        body: t("远程中转拒绝了当前网页的来源。请从 reasonix.io 打开 Web Studio 后再连接。"),
      };
    case "rate_limited": {
      const waitS = retryAfterS && retryAfterS > 0 ? Math.min(Math.ceil(retryAfterS), 600) : 0;
      return {
        action: "reconnect",
        retryWhenOnline: false,
        waitS,
        title: t("尝试次数过多"),
        body: waitS > 0 ? t("请求过于频繁。请等待 {n} 秒后重新连接。", { n: waitS }) : t("请求过于频繁。请稍等片刻后重新连接。"),
      };
    }
    case "refused":
      return {
        ...reconnect,
        title: t("中转拒绝了这次连接"),
        body: t("连接凭证可能已过期，或这台电脑上同时连接的设备已满。请重新连接；仍然失败请等一分钟再试。"),
      };
    case "idle":
      return {
        ...reconnect,
        title: t("长时间没有操作"),
        body: t("为节省资源，中转已回收这条空闲连接。点击重新连接即可继续。"),
      };
    case "unknown":
      return { ...reconnect, title: t("远程连接失败"), body: t("远程中转服务暂时不可用，请稍后重试。") };
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
