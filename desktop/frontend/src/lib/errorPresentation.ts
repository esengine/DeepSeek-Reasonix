import { t, getLocale, type DictKey, type Translator, type Locale } from "./i18n";
import type { WireEvent } from "./types";

export interface ErrorPresentation { summary: string; detail: string }

// Presentation only: callers must retain the original error for control flow,
// diagnostics and persistence. Never send translated messages back to tools.
export function errorDetail(error: unknown): string {
  if (error instanceof Error) return error.message || error.name;
  if (typeof error === "string") return error;
  if (error == null) return "";
  if (typeof error === "object") {
    const record = error as { message?: unknown; code?: unknown };
    if (typeof record.message === "string") return typeof record.code === "string" ? `${record.code}: ${record.message}` : record.message;
    try { return JSON.stringify(error); } catch { return String(error); }
  }
  return String(error);
}

const codeKeys: Record<string, DictKey> = {
  ECONNREFUSED: "error.connection", ENOTFOUND: "error.dns", EAI_AGAIN: "error.dns",
  ECONNRESET: "error.interrupted", ETIMEDOUT: "error.timeout", ABORT_ERR: "error.cancelled",
  ENOENT: "error.fileMissing", EACCES: "error.permission", EPERM: "error.permission",
  ENOSPC: "error.diskFull", EROFS: "error.readOnly", context_length_exceeded: "error.context",
  invalid_api_key: "error.auth", insufficient_quota: "error.quota", rate_limit_exceeded: "error.rateLimit",
  stream_interrupted: "error.interrupted", empty_response: "error.empty", stale_generation: "error.conflict",
  provider_connection: "error.connection", cancelled: "error.cancelled",
  transport_protocol: "error.transportProtocol",
  request_send_timeout: "error.requestSendTimeout",
  transport_eof: "error.transportEOF",
  transport_reset: "error.transportReset",
  transport_timeout: "error.timeout",
  transport_dns: "error.dns",
  no_deliverable_output: "error.noDeliverable",
  image_input: "error.imageInput",
};

// Narrow legacy matches bridge errors from older services and OS libraries.
// Unknown errors deliberately keep a neutral summary instead of guessing.
const legacyKeys: [RegExp, DictKey][] = [
  [/\bno_deliverable_output\b/, "error.noDeliverable"],
  [/\b(?:insufficient_quota|insufficient balance|quota exhausted)\b|余额不足|額度不足|额度不足/i, "error.quota"],
  [/\b(?:context_length_exceeded|context (?:window|length).*(?:exceed|limit)|maximum context length)\b|超出.*上下文/i, "error.context"],
  [/\b(?:ENOSPC|no space left on device|disk full)\b/i, "error.diskFull"],
  [/\b(?:EACCES|EPERM|permission denied|operation not permitted)\b/i, "error.permission"],
  [/\b(?:ENOENT|no such file or directory|file not found)\b/i, "error.fileMissing"],
  [/\b(?:EROFS|read-only file system)\b/i, "error.readOnly"],
  [/\b(?:ENOTFOUND|EAI_AGAIN|no such host|name resolution failed)\b/i, "error.dns"],
  [/\b(?:ECONNREFUSED|connection refused|failed to fetch|network request failed|networkerror)\b/i, "error.connection"],
  [/\b(?:ECONNRESET|unexpected EOF|premature EOF|connection reset|model stream (?:interrupted|disconnected))\b/i, "error.interrupted"],
  [/\b(?:ETIMEDOUT|deadline exceeded|timed out|(?:request|connection|response|stream)[ -]timeout)\b/i, "error.timeout"],
  [/\b(?:context canceled|context cancelled|AbortError|operation (?:aborted|cancelled|canceled))\b/i, "error.cancelled"],
  [/\b(?:empty (?:provider )?response|no content returned|zero.content response)\b/i, "error.empty"],
  [/\b(?:invalid_api_key|invalid api key|authentication failed|unauthorized)\b/i, "error.auth"],
  [/\b(?:rate_limit_exceeded|rate limit exceeded|too many requests)\b/i, "error.rateLimit"],
  [/\b(?:session changed on disk|workspace mutation conflicts with persisted state|stale generation|runtime changed|writer is owned by another runtime)\b/i, "error.conflict"],
  [/\b(?:unknown tool|tool not found|MCP server.*(?:unavailable|not connected|failed to start))\b/i, "error.toolUnavailable"],
  [/\b(?:invalid JSON|unexpected token.*JSON|JSON.*syntax|invalid header field name)\b/i, "error.invalidInput"],
];

function statusKey(status: number): DictKey | undefined {
  if (status === 401) return "error.auth";
  if (status === 402) return "error.quota";
  if (status === 403) return "error.forbidden";
  if (status === 404) return "error.endpoint";
  if (status === 408 || status === 504) return "error.timeout";
  if (status === 409) return "error.conflict";
  if (status === 413) return "error.tooLarge";
  if (status === 429) return "error.rateLimit";
  if (status >= 500 && status <= 599) return "error.service";
  if (status === 400 || status === 422) return "error.invalidInput";
}

export function presentError(error: unknown, translate: Translator = t, locale: Locale = getLocale(), diagnostic?: WireEvent["diagnostic"]): ErrorPresentation {
  const detail = errorDetail(error).trim();
  if (diagnostic?.kind === "transport_protocol") {
    return { summary: translate("error.transportProtocol"), detail: [detail, diagnostic.transportCode ? `HTTP/2: ${diagnostic.transportCode}` : ""].filter(Boolean).join("\n") };
  }
  if (diagnostic?.kind && codeKeys[diagnostic.kind]) {
    return { summary: translate(codeKeys[diagnostic.kind]), detail };
  }
  const structured = error && typeof error === "object" ? error as { code?: unknown; status?: unknown } : undefined;
  const code = typeof structured?.code === "string" ? structured.code : detail;
  const explicitStatus = typeof structured?.status === "number" ? structured.status : 0;
  // Only interpret explicitly labelled HTTP status codes, never arbitrary
  // numbers in paths, command output or provider messages.
  const status = explicitStatus || Number(/^provider_http_([45]\d\d)$/.exec(code)?.[1] ?? /\b(?:HTTP(?:\/\d(?:\.\d)?)?\s*|status(?: code)?[:= ]+)([45]\d\d)\b/i.exec(detail)?.[1] ?? 0);
  const key = codeKeys[code] ?? (status && ![400, 413, 429].includes(status) ? statusKey(status) : undefined)
    ?? legacyKeys.find(([pattern]) => pattern.test(detail))?.[1] ?? statusKey(status);
  if (key) return { summary: translate(key), detail };

  // Preserve existing localized, actionable copy. A translated wrapper around
  // an English exception is only a summary; keep the complete cause in details.
  const firstLine = detail.split("\n")[0] ?? "";
  if (locale === "en" && /[\u3400-\u9fff]/u.test(firstLine)) {
    return { summary: translate("error.unknown"), detail };
  }
  if (locale === "en" && firstLine) {
    const summary = firstLine.length > 240 ? `${firstLine.slice(0, 239)}…` : firstLine;
    return { summary, detail: summary === detail ? "" : detail };
  }
  const localizedLine = /^[^:\n]{1,80}:\s*([\u3400-\u9fff].*)$/u.exec(firstLine)?.[1] ?? firstLine;
  if (/^[\u3400-\u9fff]/u.test(localizedLine)) {
    const boundary = /[：:]\s*(?=[A-Za-z{\[])/u.exec(localizedLine);
    const summary = boundary ? localizedLine.slice(0, boundary.index) : localizedLine;
    return { summary, detail: summary === detail ? "" : detail };
  }
  return { summary: translate("error.unknown"), detail };
}
