import { useT } from "../lib/i18n";
import type { CommandInfo } from "../lib/types";
import { VirtualMenu } from "./VirtualMenu";

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
  // builtin commands get no tag; custom (project) and mcp commands are labelled.
  const kindTag = (kind: CommandInfo["kind"]) =>
    kind === "custom"
      ? t("slash.project")
      : kind === "mcp"
        ? t("slash.mcp")
        : kind === "skill"
          ? t("slash.skill")
          : "";
  return (
    <VirtualMenu
      items={items}
      activeIndex={activeIndex}
      itemKey={(c) => c.kind + ":" + c.name}
      renderItem={(c, i) => (
        <button
          role="option"
          aria-selected={i === activeIndex}
          className={`slashmenu__item ${i === activeIndex ? "slashmenu__item--active" : ""}`}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(c);
          }}
          onMouseMove={() => onHover(i)}
        >
          <span className="slashmenu__name">/{c.name}</span>
          {c.hint && <span className="slashmenu__hint">{c.hint}</span>}
          <span className="slashmenu__desc">{c.description}</span>
          {kindTag(c.kind) && <span className="slashmenu__kind">{kindTag(c.kind)}</span>}
        </button>
      )}
    />
  );
}
