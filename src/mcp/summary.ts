import type { InspectionReport } from "./inspect.js";
import type { BridgeEnv, McpClientHost } from "./registry.js";
import type { GetPromptResult, ReadResourceResult } from "./types.js";

export interface McpServerSummary {
  label: string;
  spec: string;
  toolCount: number;
  report: InspectionReport;
  host: McpClientHost;
  bridgeEnv: BridgeEnv;
  readResource(uri: string): Promise<ReadResourceResult>;
  getPrompt(name: string, args?: Record<string, string>): Promise<GetPromptResult>;
}

export function buildMcpServerSummary(opts: {
  label: string;
  spec: string;
  toolCount: number;
  report: InspectionReport;
  host: McpClientHost;
  bridgeEnv: BridgeEnv;
}): McpServerSummary {
  return {
    label: opts.label,
    spec: opts.spec,
    toolCount: opts.toolCount,
    report: opts.report,
    host: opts.host,
    bridgeEnv: opts.bridgeEnv,
    readResource(uri) {
      return opts.host.client.readResource(uri);
    },
    getPrompt(name, args) {
      return args !== undefined
        ? opts.host.client.getPrompt(name, args)
        : opts.host.client.getPrompt(name);
    },
  };
}
