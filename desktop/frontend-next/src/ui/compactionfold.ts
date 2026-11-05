import type { Item } from "../state/session";

/** Where the visible transcript starts once earlier compactions fold what they
 *  replaced. One fold only: the content up to the second-latest compaction goes
 *  behind it, so the stretch the latest compaction itself closed stays on screen
 *  and a fresh compaction never leaves the page empty. A compaction that folded
 *  nothing, or has not finished, is not a boundary. */
export function compactionFold(items: readonly Item[]): { cut: number; folded: number } {
  const marks: number[] = [];
  items.forEach((item, i) => {
    if (item.t === "compaction" && item.done && (item.c.messages ?? 0) > 0) marks.push(i);
  });
  if (marks.length < 2) return { cut: 0, folded: 0 };
  const cut = marks[marks.length - 2];
  const folded = items.slice(0, cut).filter((item) => item.t === "user" || item.t === "say").length;
  return { cut, folded };
}
