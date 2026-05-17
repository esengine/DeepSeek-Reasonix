import type { McpServerSpec } from "./spec.js";
import { SseTransport } from "./sse.js";
import { type McpTransport, StdioTransport } from "./stdio.js";
import { StreamableHttpTransport } from "./streamable-http.js";

export interface BuildTransportOptions {
  /** Stdio-only env overlay — merged over process.env. SSE/Streamable-HTTP ignore it. */
  env?: Record<string, string>;
  /** SSE / Streamable-HTTP only. Ignored by stdio. */
  headers?: Record<string, string>;
}

export function buildTransportFromSpec(
  spec: McpServerSpec,
  opts: BuildTransportOptions = {},
): McpTransport {
  if (spec.transport === "sse") {
    return new SseTransport({ url: spec.url, headers: opts.headers ?? spec.headers });
  }
  if (spec.transport === "streamable-http") {
    return new StreamableHttpTransport({
      url: spec.url,
      headers: opts.headers ?? spec.headers,
    });
  }
  return new StdioTransport({
    command: spec.command,
    args: spec.args,
    env: opts.env ?? spec.env,
  });
}
