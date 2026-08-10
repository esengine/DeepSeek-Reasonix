import type { RootContent as HastRootContent } from "hast";
import type { SelectionProjectionBlock } from "./markdownSelectionProjection";

/** Allocation-free byte-weight estimate for parsed transcript Markdown. */
export function estimateHastBytes(blocks: readonly SelectionProjectionBlock[]): number {
  let bytes = 0;
  const walk = (node: HastRootContent): void => {
    bytes += 48;
    if (node.type === "text" || node.type === "comment") {
      bytes += (node.value?.length ?? 0) * 2;
      return;
    }
    if (node.type === "element") {
      bytes += node.tagName.length * 2;
      for (const key in node.properties) {
        if (!Object.prototype.hasOwnProperty.call(node.properties, key)) continue;
        const value = node.properties[key];
        bytes += key.length * 2;
        if (typeof value === "string") bytes += value.length * 2;
        else if (Array.isArray(value)) bytes += value.length * 16;
        else bytes += 16;
      }
      for (const child of node.children) walk(child);
    }
  };
  for (const block of blocks) {
    if (block.virtualTable) {
      bytes += block.virtualTable.header.reduce((total, cell) => total + 24 + cell.length * 2, 0);
      bytes += block.virtualTable.rows.reduce(
        (total, row) => total + 24 + row.reduce((rowTotal, cell) => rowTotal + 24 + cell.length * 2, 0),
        0,
      );
    }
    for (const child of block.children) walk(child);
  }
  return bytes;
}
