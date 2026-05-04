import type { InspectionReport } from "./inspect.js";
import type { BridgeEnv, McpClientHost } from "./registry.js";

export interface McpServerSummary {
  label: string;
  spec: string;
  toolCount: number;
  report: InspectionReport;
  host: McpClientHost;
  bridgeEnv: BridgeEnv;
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
  };
}
