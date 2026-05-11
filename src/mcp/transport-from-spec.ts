import type { McpSpec } from "./spec.js";
import { SseTransport } from "./sse.js";
import { type McpTransport, StdioTransport } from "./stdio.js";
import { StreamableHttpTransport } from "./streamable-http.js";

export interface BuildTransportOptions {
  /** Stdio-only env overlay — merged over process.env. SSE/Streamable-HTTP ignore it. */
  env?: Record<string, string>;
}

export function buildTransportFromSpec(
  spec: McpSpec,
  opts: BuildTransportOptions = {},
): McpTransport {
  if (spec.transport === "sse") return new SseTransport({ url: spec.url });
  if (spec.transport === "streamable-http") return new StreamableHttpTransport({ url: spec.url });
  return new StdioTransport({ command: spec.command, args: spec.args, env: opts.env });
}
