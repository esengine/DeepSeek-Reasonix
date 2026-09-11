import type { Item } from "./useController";

const ARCHIVED_TOOL_ARG_LIMIT = 200;

function archivedToolArgs(args: string): string {
  return args && args.length > ARCHIVED_TOOL_ARG_LIMIT ? args.slice(0, ARCHIVED_TOOL_ARG_LIMIT) + "…" : args;
}

function latestCanonicalTodoToolIndex(items: Item[]): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.kind === "tool" && item.name === "todo_write" && !item.parentId && item.status === "done" && !item.error) return index;
  }
  return -1;
}

export function compactArchivedToolItems(items: Item[]): Item[] {
  const canonicalTodoIndex = latestCanonicalTodoToolIndex(items);
  return items.map((item, index) => {
    if (item.kind !== "tool" || item.status === "running") return item;
    const nextArgs = index === canonicalTodoIndex ? item.args : archivedToolArgs(item.args);
    if (nextArgs === item.args && item.output === undefined && item.dataArchived === true) return item;
    return { ...item, args: nextArgs, output: undefined, dataArchived: true };
  });
}
