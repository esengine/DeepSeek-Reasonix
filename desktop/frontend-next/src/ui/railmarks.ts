import type { Item } from "../state/session";
import type { RailMark } from "./Rail";

/** What you said, and where it sits. Derived from the same blocks the
 *  transcript renders, so a mark always knows which block holds it — that is
 *  what makes it locatable while the message itself is unmounted.
 *
 *  `at` is the rendered position; `msgIndex` is the message's place in the
 *  session log, which survives a reload. */
export function railMarks(blocks: Item[][], filesOf: (id: string) => number): RailMark[] {
  const marks: RailMark[] = [];
  let offset = 0;
  blocks.forEach((block, b) => {
    const base = offset;
    offset += block.length;
    block.forEach((it, i) => {
      if (it.t !== "user") return;
      marks.push({
        id: it.id,
        text: it.text,
        at: base + i,
        msgIndex: it.msgIndex,
        block: b,
        within: i,
        of: block.length,
        files: filesOf(it.id),
      });
    });
  });
  return marks;
}
