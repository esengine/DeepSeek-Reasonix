import type { Item } from "../state/session";

type Row = { item: Item; activity?: Item[] } | { activity: Item[] };

/** drawn reports whether a sentence has anything to show. One that does not
 *  draws nothing, so it cannot host work either. */
export function drawn(item: Item): boolean {
  return item.t === "say" && Boolean(item.text.trim() || item.reasoning?.trim());
}

/** transcriptRows arranges a transcript the way it reads: a turn is one user
 *  message and everything done about it, consecutive tool steps fold into one
 *  disclosure, and each run of work sits under the sentence it followed — a turn
 *  that answers several times keeps each answer with its own work. */
export function transcriptRows(all: Item[]): Row[] {
  // A queued line has not happened yet, so it is the composer's to show and not
  // this list's. Leaving it here also split the running turn at the moment of
  // typing and handed its work to a line the model had not read.
  const items = all.filter((i) => !(i.t === "user" && i.pending));
  const rows: Row[] = [];
  for (let at = 0; at < items.length;) {
    let end = at + 1;
    while (end < items.length && items[end].t !== "user") end++;
    const turn: Row[] = [];
    for (const item of items.slice(at, end)) {
      const work = item.t === "tool" || item.t === "reads";
      const last = turn[turn.length - 1];
      if (work && last && "activity" in last && !("item" in last)) last.activity.push(item);
      else turn.push(work ? { activity: [item] } : { item });
    }
    for (let i = turn.length - 1; i >= 0; i--) {
      const row = turn[i];
      if ("item" in row) continue;
      // The sentence this work followed, else the one it was for. A message the
      // model sent with no words yet is the speaker only when neither exists.
      const host = nearest(turn, i, drawn) ?? nearest(turn, i, (item) => item.t === "say") ?? -1;
      if (host < 0) continue;
      const into = turn[host] as { item: Item; activity?: Item[] };
      into.activity = [...row.activity, ...(into.activity ?? [])];
      turn.splice(i, 1);
    }
    rows.push(...turn);
    at = end;
  }
  return rows;
}

function nearest(turn: Row[], i: number, fits: (item: Item) => boolean): number | undefined {
  for (let j = i - 1; j >= 0; j--) {
    const row = turn[j];
    if ("item" in row && fits(row.item)) return j;
  }
  // A turn that opens with work has no sentence before it yet, and the one it
  // was for is the first that follows.
  for (let j = i + 1; j < turn.length; j++) {
    const row = turn[j];
    if ("item" in row && fits(row.item)) return j;
  }
  return undefined;
}
