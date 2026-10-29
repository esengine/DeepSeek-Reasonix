import { t } from "../i18n";
import { chipId, elementInstruction, type Chip, type PickedSnapshot } from "./composerChips";
import { PickIcon } from "./pickicon";

// One shot row for a picked element: the mark, the size, and the one thing a
// person does next with it.
export function ElementShot({ picked, name, onInsert }: { picked: PickedSnapshot; name: string; onInsert: (text: string) => void }) {
  return (
    <>
      <span className="glyph" aria-hidden="true"><PickIcon size={12} /></span>
      <span className="meta">
        <span className="nm" title={picked.selector}>{name}</span>
        <span className="sz">{t("网页元素")}</span>
        <button className="undo" onClick={() => onInsert(elementInstruction(picked))}>{t("让 Agent 修改")}</button>
      </span>
    </>
  );
}

// The same element picked twice is still one chip.
export function withElement(prev: Chip[], picked: PickedSnapshot): Chip[] {
  return prev.some((c) => c.k === "element" && c.picked.url === picked.url && c.picked.selector === picked.selector)
    ? prev
    : [...prev, { k: "element", id: chipId(), picked }];
}
