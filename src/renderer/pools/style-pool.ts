export interface AnsiCode {
  readonly apply: string;
  readonly revert: string;
}

const EMPTY_KEY = "";

export class StylePool {
  private readonly stacks: ReadonlyArray<AnsiCode>[] = [[]];
  private readonly idsByKey = new Map<string, number>([[EMPTY_KEY, 0]]);
  private readonly transitionCache = new Map<number, string>();
  readonly none = 0;

  intern(codes: ReadonlyArray<AnsiCode>): number {
    const key = stackKey(codes);
    const existing = this.idsByKey.get(key);
    if (existing !== undefined) return existing;
    const id = this.stacks.length;
    this.stacks.push(codes);
    this.idsByKey.set(key, id);
    return id;
  }

  transition(fromId: number, toId: number): string {
    if (fromId === toId) return "";
    const cacheKey = (fromId << 16) | toId;
    const cached = this.transitionCache.get(cacheKey);
    if (cached !== undefined) return cached;

    const from = this.stacks[fromId] ?? [];
    const to = this.stacks[toId] ?? [];
    const fromApplies = new Set(from.map((c) => c.apply));
    const toApplies = new Set(to.map((c) => c.apply));

    let out = "";
    for (const c of from) {
      if (!toApplies.has(c.apply)) out += c.revert;
    }
    for (const c of to) {
      if (!fromApplies.has(c.apply)) out += c.apply;
    }

    this.transitionCache.set(cacheKey, out);
    return out;
  }

  get size(): number {
    return this.stacks.length;
  }
}

function stackKey(codes: ReadonlyArray<AnsiCode>): string {
  if (codes.length === 0) return EMPTY_KEY;
  // Sort so {RED, BOLD} and {BOLD, RED} hash to the same id — style identity is the SET of codes.
  const applies = codes.map((c) => c.apply);
  applies.sort();
  return applies.join("\x00");
}
