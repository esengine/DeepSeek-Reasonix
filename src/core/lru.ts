/** Insertion-ordered Map ⇒ LRU: re-insert on hit promotes the entry; eviction pops the oldest key. */
export class LruCache<K, V> {
  private readonly map = new Map<K, V>();
  constructor(private readonly limit: number) {}

  get(key: K): V | undefined {
    if (!this.map.has(key)) return undefined;
    const v = this.map.get(key) as V;
    this.map.delete(key);
    this.map.set(key, v);
    return v;
  }

  set(key: K, value: V): void {
    if (this.map.has(key)) {
      this.map.delete(key);
    } else if (this.map.size >= this.limit) {
      const oldest = this.map.keys().next().value;
      if (oldest !== undefined) this.map.delete(oldest);
    }
    this.map.set(key, value);
  }

  get size(): number {
    return this.map.size;
  }

  clear(): void {
    this.map.clear();
  }
}

/** LruCache with a per-entry TTL — a stale hit is treated as a miss and evicted. */
export class TtlLruCache<K, V> {
  private readonly inner: LruCache<K, { v: V; expiresAt: number }>;
  constructor(
    limit: number,
    private readonly ttlMs: number,
  ) {
    this.inner = new LruCache(limit);
  }

  get(key: K): V | undefined {
    const e = this.inner.get(key);
    if (!e) return undefined;
    if (e.expiresAt <= Date.now()) return undefined;
    return e.v;
  }

  set(key: K, value: V): void {
    this.inner.set(key, { v: value, expiresAt: Date.now() + this.ttlMs });
  }

  clear(): void {
    this.inner.clear();
  }
}
