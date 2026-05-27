import { stringWidth } from './stringWidth.js';

/** Memoised per-line `stringWidth` lookups. */
const CACHE_LIMIT = 4096;

const cache = new Map<string, number>();

export function lineWidth(line: string): number {
  const hit = cache.get(line);
  if (hit !== undefined) return hit;

  const width = stringWidth(line);

  if (cache.size >= CACHE_LIMIT) {
    cache.clear();
  }
  cache.set(line, width);
  return width;
}
