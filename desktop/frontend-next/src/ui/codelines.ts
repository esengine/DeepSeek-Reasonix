import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";

type Row = ReactNode[];

function join(head: Row[], tail: Row[]): Row[] {
  return [...head.slice(0, -1), [...head[head.length - 1], ...tail[0]], ...tail.slice(1)];
}

function rowsOf(node: ReactNode): Row[] {
  if (node == null || typeof node === "boolean") return [[]];
  if (typeof node === "number") return [[String(node)]];
  if (typeof node === "string") return node.split("\n").map((piece) => (piece === "" ? [] : [piece]));
  if (Array.isArray(node)) return node.reduce<Row[]>((all, child) => join(all, rowsOf(child)), [[]]);
  if (!isValidElement(node)) return [[]];
  const element = node as ReactElement<{ children?: ReactNode }>;
  const inner = rowsOf(element.props.children);
  if (inner.length === 1) return [[element]];
  return inner.map((row, i) => (row.length === 0 ? [] : [cloneElement(element, { key: `${element.key ?? "n"}-${i}` }, ...row)]));
}

/** The rows of a highlighted tree, one per source line. An element that spans a
 *  newline is repeated in every row it touches, so each row stands on its own. */
export function splitLines(tree: ReactNode): ReactNode[][] {
  return rowsOf(tree);
}
