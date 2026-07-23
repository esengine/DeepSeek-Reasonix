import { useT, type Translator } from "../lib/i18n";
import type { CommandInfo } from "../lib/types";
import { VirtualMenu } from "./VirtualMenu";

export function slashCommandKindTag(command: CommandInfo, t: Translator): string {
  if (command.plugin) {
    return t("slash.plugin", { name: command.plugin });
  }
  if (command.kind === "custom") return t("slash.project");
  if (command.kind === "mcp") return t("slash.mcp");
  if (command.kind === "subagent") return t("slash.subagent");
  if (command.kind === "skill") return t("slash.skill");
  return "";
}

type SlashCommandGroup = "actions" | "management" | "subagents" | "skills" | "integrations";

type SlashMenuRow =
  | { type: "group"; group: SlashCommandGroup; label: string }
  | { type: "command"; command: CommandInfo; commandIndex: number };

const slashCommandGroups: SlashCommandGroup[] = ["actions", "subagents", "skills", "integrations", "management"];
const slashCommandGroupOrder = new Map(slashCommandGroups.map((group, index) => [group, index]));
const fallbackQuickActions = new Set(["new", "clear", "compact", "model", "effort", "goal"]);

export function slashCommandGroup(command: CommandInfo): SlashCommandGroup {
  if (command.group && slashCommandGroupOrder.has(command.group)) return command.group;
  if (command.kind === "subagent") return "subagents";
  if (command.kind === "mcp") return "integrations";
  if (command.kind === "skill" || command.kind === "custom") return "skills";
  return fallbackQuickActions.has(command.name) ? "actions" : "management";
}

export function sortSlashCommandsForMenu(commands: CommandInfo[]): CommandInfo[] {
  return commands
    .map((command, index) => ({ command, index }))
    .sort((a, b) => {
      const groupDelta = (slashCommandGroupOrder.get(slashCommandGroup(a.command)) ?? 0)
        - (slashCommandGroupOrder.get(slashCommandGroup(b.command)) ?? 0);
      return groupDelta || a.index - b.index;
    })
    .map(({ command }) => command);
}

// SlashMenu is the "/" autocomplete dropdown above the composer. Presentational:
// the Composer owns filtering, the active index, and key handling; this renders
// the (virtualized) list and reports hover/pick. Uses mousedown (not click) so
// picking an item doesn't blur the textarea first.
export function SlashMenu({
  items,
  activeIndex,
  onPick,
  onHover,
}: {
  items: CommandInfo[];
  activeIndex: number;
  onPick: (c: CommandInfo) => void;
  onHover: (i: number) => void;
}) {
  const t = useT();
  const grouped = new Map<SlashCommandGroup, Array<{ command: CommandInfo; commandIndex: number }>>();
  items.forEach((command, commandIndex) => {
    const group = slashCommandGroup(command);
    const groupItems = grouped.get(group) ?? [];
    groupItems.push({ command, commandIndex });
    grouped.set(group, groupItems);
  });
  const rows: SlashMenuRow[] = slashCommandGroups.flatMap((group) => {
    const groupItems = grouped.get(group);
    if (!groupItems?.length) return [];
    return [
      { type: "group" as const, group, label: t(`slash.group.${group}`) },
      ...groupItems.map(({ command, commandIndex }) => ({ type: "command" as const, command, commandIndex })),
    ];
  });
  const activeRowIndex = rows.findIndex((row) => row.type === "command" && row.commandIndex === activeIndex);
  return (
    <VirtualMenu
      items={rows}
      activeIndex={activeRowIndex}
      itemKey={(row) => row.type === "group" ? `group:${row.group}` : `${row.command.kind}:${row.command.name}:${row.commandIndex}`}
      estimateSize={(row) => row.type === "group" ? 26 : 34}
      renderItem={(row) => row.type === "group" ? (
        <div className="slashmenu__group" role="separator" aria-label={row.label}>
          {row.label}
        </div>
      ) : (
        <button
          role="option"
          aria-selected={row.commandIndex === activeIndex}
          className={`slashmenu__item ${row.commandIndex === activeIndex ? "slashmenu__item--active" : ""}`}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(row.command);
          }}
          onMouseMove={() => onHover(row.commandIndex)}
        >
          <span className="slashmenu__name">/{row.command.name}</span>
          {row.command.hint && <span className="slashmenu__hint">{row.command.hint}</span>}
          <span className="slashmenu__desc">{row.command.description}</span>
          {slashCommandKindTag(row.command, t) && <span className="slashmenu__kind">{slashCommandKindTag(row.command, t)}</span>}
        </button>
      )}
    />
  );
}
