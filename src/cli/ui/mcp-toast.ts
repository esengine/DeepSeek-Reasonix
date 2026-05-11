/** One-line warn toast emitted when an MCP server's p95 crosses the slow threshold (design §32). */

import { t } from "../../i18n/index.js";

export interface McpSlowToast {
  name: string;
  p95Ms: number;
  sampleSize: number;
}

export function formatMcpSlowToast(tst: McpSlowToast): string {
  const seconds = (tst.p95Ms / 1000).toFixed(1);
  return t("mcpHealth.slowToast", { name: tst.name, seconds, sampleSize: tst.sampleSize });
}
