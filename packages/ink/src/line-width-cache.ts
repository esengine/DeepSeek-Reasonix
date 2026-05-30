import { stringWidth } from './stringWidth.js';

/**
 * Per-line `stringWidth` cache with LRU eviction.
 *
 * The renderer calls this ~100k times per frame. Temporal locality is high:
 * the same lines appear across consecutive frames. An LRU keeps hot entries
 * resident instead of the old sawtooth pattern (fill → clear-all → cold-start).
 *
 * A plain Map provides insertion order, which is sufficient for LRU:
 * - On hit: delete + re-set promotes the entry.
 * - On miss with a full cache: evict the oldest key (first in iteration order).
 */
const CACHE_LIMIT = 4096;

const cache = new Map<string, number>();

export function lineWidth(line: string): number {
  const hit = cache.get(line);
  if (hit !== undefined) {
    // Promote: move to the end so it survives the next eviction scan.
    cache.delete(line);
    cache.set(line, hit);
    return hit;
  }

  const width = stringWidth(line);

  if (cache.size >= CACHE_LIMIT) {
    // Evict only the single least-recently-used entry — not the entire cache.
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
  cache.set(line, width);
  return width;
}
