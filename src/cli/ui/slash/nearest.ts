export type NearestCommandOptions = {
  max?: number;
  maxDistance?: number;
};

export function nearestCommands(
  input: string,
  all: readonly string[],
  opts: NearestCommandOptions = {},
): string[] {
  if (!input) return [];
  const max = opts.max ?? 3;
  const maxDistance = Math.min(opts.maxDistance ?? 3, Math.floor(input.length / 2));
  if (max <= 0 || maxDistance <= 0) return [];
  return all
    .map((name) => ({ name, distance: levenshtein(input, name) }))
    .filter((entry) => entry.distance <= maxDistance)
    .sort((a, b) => a.distance - b.distance || a.name.localeCompare(b.name))
    .slice(0, max)
    .map((entry) => entry.name);
}

function levenshtein(a: string, b: string): number {
  if (a === b) return 0;
  if (!a) return b.length;
  if (!b) return a.length;
  let prev = Array.from({ length: b.length + 1 }, (_, i) => i);
  let next = new Array<number>(b.length + 1).fill(0);
  for (let i = 0; i < a.length; i += 1) {
    next[0] = i + 1;
    for (let j = 0; j < b.length; j += 1) {
      const cost = a[i] === b[j] ? 0 : 1;
      next[j + 1] = Math.min((next[j] ?? 0) + 1, (prev[j + 1] ?? 0) + 1, (prev[j] ?? 0) + cost);
    }
    [prev, next] = [next, prev];
  }
  return prev[b.length] ?? 0;
}
