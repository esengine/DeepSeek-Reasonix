import type { Tool } from "../../port/wire";
import { t } from "../../i18n";
import { say } from "../../i18n/kernel";

export function toolFailed(tool: Tool): boolean {
  if (tool.err) return true;
  const execution = tool.execution;
  return !!execution && ((!!execution.state && execution.state !== "completed") || (execution.exitCode ?? 0) !== 0);
}

export function toolChangedFile(tool: Tool): boolean {
  return !!tool.diff || tool.added !== undefined || tool.removed !== undefined;
}

export function toolFailureLabel(tool: Tool): string {
  const execution = tool.execution;
  if ((execution?.exitCode ?? 0) !== 0) return `exit ${execution?.exitCode}`;
  if (execution?.state && execution.state !== "completed") return execution.state;
  // The host names which refusal this was; "失败" is what is left when nobody did.
  if (tool.refusalCode) return tool.refusalCode;
  return t("失败");
}

// What the host's refusal code means, in the reader's language; empty when the
// code has no wording here and the kernel's own sentence is all there is.
export function toolRefusalReason(tool: Tool): string {
  return tool.refusalCode ? say({ code: tool.refusalCode }, "") : "";
}
