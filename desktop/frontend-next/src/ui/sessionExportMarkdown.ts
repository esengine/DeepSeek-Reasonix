/** A conversation written out as prose.
 *
 *  The record shape is the kernel's own: each line of an export is a provider
 *  message (`role`, `content`, `tool_calls`, `host_authored`), not a UI event.
 *  Everything that is not something a person said is left out on purpose — tool
 *  results, system lines, and the turns where the model only reached for a tool.
 *  The JSON export remains the one that keeps every field.
 */
import { t } from "../i18n";

interface Line {
  role?: string;
  content?: string;
  host_authored?: boolean;
}

/** What a single message contributes, or null when it contributes nothing. */
function turnOf(raw: unknown): { mine: boolean; text: string } | null {
  if (!raw || typeof raw !== "object") return null;
  const line = raw as Line;
  const text = (line.content ?? "").trim();
  // A host-authored user message is the window talking to the model, not a
  // person; it never belongs in the transcript a reader gets.
  if (line.role === "user") return line.host_authored || !text ? null : { mine: true, text };
  // An assistant turn that only carries tool_calls has no content to show.
  if (line.role === "assistant") return text ? { mine: false, text } : null;
  return null;
}

export function toMarkdown(title: string, records: unknown[]): string {
  const out: string[] = [`# ${title || t("会话")}`, ""];
  for (const record of records) {
    const turn = turnOf(record);
    if (!turn) continue;
    out.push(turn.mine ? `## ${t("我")}` : `## ${t("助手")}`, "", turn.text, "");
  }
  return out.join("\n");
}
