import { app } from "./bridge";
import type { useT } from "./i18n";
import type { MCPInstallResult, MCPServerInput } from "./types";

export function activeWorkBusyNoticeText(error: unknown, translate: ReturnType<typeof useT>): string | null {
  const message = String((error as Error)?.message ?? error ?? "").trim();
  const lower = message.toLowerCase();
  if (!lower.startsWith("active work is still running;") || !lower.includes("before changing ")) return null;

  const detail = /running=(true|false);\s*pending_prompt=(true|false);\s*background_jobs=(\d+)/i.exec(message);
  if (detail?.[2] === "true") return translate("caps.switchBusyPrompt");
  if (detail?.[1] === "true") return translate("caps.switchBusyRunning");
  const jobs = Number(detail?.[3] ?? 0);
  if (jobs > 0) return translate("caps.switchBusyJobs", { n: jobs });
  return translate("caps.switchBusy");
}

export async function installMCPServer(input: MCPServerInput): Promise<MCPInstallResult> {
  const result = await app.InstallMCPServer(input);
  if (result.state === "issue") throw new Error(result.message);
  return result;
}
