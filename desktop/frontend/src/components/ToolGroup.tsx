import { memo, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { useT } from "../lib/i18n";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import type { Item } from "../lib/useController";
import { ToolCard } from "./ToolCard";

type ToolItem = Extract<Item, { kind: "tool" }>;

export type ToolGroupKind = "explore" | "modify" | "delegate" | "shell";

const SHELL_TOOLS = new Set(["bash", "bash_output", "wait", "waitJob", "kill_shell"]);
const EXPLORE_TOOLS = new Set(["read_file", "ls", "grep", "glob", "web_fetch", "code_index", "read_skill", "connect_tool_source"]);
const MODIFY_TOOLS = new Set(["write_file", "edit_file", "multi_edit", "move_file", "delete_range", "delete_symbol", "notebook_edit"]);
const DELEGATE_TOOLS = new Set(["task", "run_skill", "explore", "research", "review", "security_review"]);

export function toolGroupKind(item: ToolItem): ToolGroupKind | null {
  if (item.parentId || item.name === "todo_write" || item.name === "exit_plan_mode") return null;
  if (SHELL_TOOLS.has(item.name)) return "shell";
  if (EXPLORE_TOOLS.has(item.name)) return "explore";
  if (MODIFY_TOOLS.has(item.name)) return "modify";
  if (DELEGATE_TOOLS.has(item.name)) return "delegate";
  return item.readOnly ? "explore" : "modify";
}

export function isCreationGroupableTool(item: ToolItem): boolean {
  return item.status !== "running" && toolGroupKind(item) !== null;
}

function count(items: ToolItem[], names: readonly string[]): number {
  return items.filter((item) => names.includes(item.name)).length;
}

function titleFor(kind: ToolGroupKind, t: ReturnType<typeof useT>): string {
  switch (kind) {
    case "explore": return t("creation.toolGroup.explore");
    case "modify": return t("creation.toolGroup.modify");
    case "delegate": return t("creation.toolGroup.delegate");
    case "shell": return t("creation.toolGroup.shell");
  }
}

function groupSummary(kind: ToolGroupKind, items: ToolItem[], t: ReturnType<typeof useT>): string {
  const parts: string[] = [];
  if (kind === "explore") {
    const readCount = count(items, ["read_file", "ls", "web_fetch", "read_skill"]);
    const searchCount = count(items, ["grep", "glob", "code_index"]);
    const otherCount = items.length - readCount - searchCount;
    if (readCount > 0) parts.push(t("creation.toolStat.read", { n: readCount }));
    if (searchCount > 0) parts.push(t("creation.toolStat.search", { n: searchCount }));
    if (otherCount > 0) parts.push(t("creation.toolStat.other", { n: otherCount }));
  } else if (kind === "modify") {
    const writeCount = count(items, ["write_file"]);
    const editCount = count(items, ["edit_file", "multi_edit", "notebook_edit"]);
    const moveCount = count(items, ["move_file"]);
    const deleteCount = count(items, ["delete_range", "delete_symbol"]);
    const otherCount = items.length - writeCount - editCount - moveCount - deleteCount;
    if (writeCount > 0) parts.push(t("creation.toolStat.write", { n: writeCount }));
    if (editCount > 0) parts.push(t("creation.toolStat.edit", { n: editCount }));
    if (moveCount > 0) parts.push(t("creation.toolStat.move", { n: moveCount }));
    if (deleteCount > 0) parts.push(t("creation.toolStat.delete", { n: deleteCount }));
    if (otherCount > 0) parts.push(t("creation.toolStat.other", { n: otherCount }));
  } else if (kind === "delegate") {
    const taskCount = count(items, ["task", "run_skill", "explore", "research", "review", "security_review"]);
    const otherCount = items.length - taskCount;
    if (taskCount > 0) parts.push(t("creation.toolStat.task", { n: taskCount }));
    if (otherCount > 0) parts.push(t("creation.toolStat.other", { n: otherCount }));
  } else {
    const commandCount = count(items, ["bash"]);
    const checkCount = count(items, ["bash_output", "wait", "waitJob"]);
    const stopCount = count(items, ["kill_shell"]);
    const otherCount = items.length - commandCount - checkCount - stopCount;
    if (commandCount > 0) parts.push(t("creation.toolStat.command", { n: commandCount }));
    if (checkCount > 0) parts.push(t("creation.toolStat.check", { n: checkCount }));
    if (stopCount > 0) parts.push(t("creation.toolStat.stop", { n: stopCount }));
    if (otherCount > 0) parts.push(t("creation.toolStat.other", { n: otherCount }));
  }
  return parts.join(", ");
}

function titleCaseName(name: string): string {
  return name
    .replace(/[-_]+/g, " ")
    .split(" ")
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(" ");
}

function toolDisplayName(name: string): string {
  switch (name) {
    case "read_file": return "Read";
    case "ls": return "List";
    case "web_fetch": return "Web Fetch";
    case "code_index": return "Code Index";
    case "write_file": return "Write";
    case "edit_file": return "Edit";
    case "multi_edit": return "Multi Edit";
    case "move_file": return "Move";
    case "bash": return "Shell";
    case "bash_output": return "Shell Output";
    case "kill_shell": return "Kill Shell";
    case "wait":
    case "waitJob": return "Wait";
    default: return titleCaseName(name);
  }
}

export const ToolGroup = memo(function ToolGroup({
  kind,
  items,
  subcalls,
  tabId,
}: {
  kind: ToolGroupKind;
  items: ToolItem[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  tabId?: string;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  useGSAPCollapse(bodyRef, open);

  if (items.length === 0) return null;

  return (
    <div className={`tool-group tool-group--${kind}${open ? " tool-group--open" : ""}`} data-kind={kind} data-entrance={items[0]?.id}>
      <button type="button" className="tool-group__head" onClick={() => setOpen((value) => !value)} aria-expanded={open}>
        <span className="tool-group__title">{titleFor(kind, t)}</span>
        <span className="tool-group__summary">{groupSummary(kind, items, t)}</span>
        <ChevronRight className={`tool-group__chevron${open ? " tool-group__chevron--open" : ""}`} size={12} />
      </button>
      <div ref={bodyRef} className="tool-group__body">
        {items.map((item) => (
          <ToolCard key={item.id} item={item} subcalls={subcalls.get(item.id)} tabId={tabId} displayName={toolDisplayName(item.name)} />
        ))}
      </div>
    </div>
  );
});
