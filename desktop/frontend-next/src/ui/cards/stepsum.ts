import { t } from "../../i18n";
import type { StateTool } from "../../state/outputcap";

const lines = (text: string) => text.replace(/\n+$/, "").split("\n");

/** What a finished step came to, for the right of its row: the exit status of a
 *  command that failed, the lines a read handed over, the matches a search
 *  found. Empty when the call's shape says none of these. */
export function stepSummary(tool: StateTool, verb: string | null, failed: boolean): string {
  const code = tool.execution?.exitCode;
  if (failed) return code !== undefined && code !== 0 ? t("panels.exit_code", { code }) : "";
  if (!tool.output) return "";
  if (verb === "chat.read") return t("chat.n_lines", { n: lines(tool.output).length });
  if (verb === "chat.search") return t("chat.n_matches", { n: lines(tool.output).filter((l) => l.trim()).length });
  return "";
}
