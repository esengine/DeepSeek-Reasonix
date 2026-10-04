import { frame, type NewEvent, type OpsEvent } from "./events";

// The slice of DurableObjectStorage the core needs, so it can be tested with a Map.
export interface Store {
  get<T>(key: string): Promise<T | undefined>;
  put(key: string, value: unknown): Promise<void>;
  delete(keys: string | string[]): Promise<unknown>;
  list<T>(options: { prefix: string; start?: string; limit?: number }): Promise<Map<string, T>>;
}

export const RING_MAX = 500;
export const MAX_AGE_MS = 7 * 24 * 3600 * 1000;
const DELIVERIES_MAX = 1000;
const EMITS_PER_MINUTE = 60;
const key = (id: number) => `e:${String(id).padStart(12, "0")}`;

export interface PushResult {
  accepted: boolean;
  reason?: "duplicate" | "rate";
  event?: OpsEvent;
}

// Ordered, bounded event log plus the "delivered up to" cursor: an event that
// arrives while no listener is connected is replayed on the next connect.
export class HubCore {
  constructor(private readonly store: Store, private readonly now: () => number = Date.now) {}

  async push(input: NewEvent, opts: { delivery?: string; rateKey?: string } = {}): Promise<PushResult> {
    if (opts.delivery) {
      const seen = (await this.store.get<string[]>("deliveries")) ?? [];
      if (seen.includes(opts.delivery)) return { accepted: false, reason: "duplicate" };
      seen.push(opts.delivery);
      await this.store.put("deliveries", seen.slice(-DELIVERIES_MAX));
    }
    if (opts.rateKey) {
      const minute = Math.floor(this.now() / 60000);
      const bucket = (await this.store.get<{ minute: number; n: number }>(`rate:${opts.rateKey}`)) ?? { minute, n: 0 };
      const n = bucket.minute === minute ? bucket.n + 1 : 1;
      if (n > EMITS_PER_MINUTE) return { accepted: false, reason: "rate" };
      await this.store.put(`rate:${opts.rateKey}`, { minute, n });
    }
    const id = ((await this.store.get<number>("next")) ?? 1);
    const event: OpsEvent = { ...input, id, ts: new Date(this.now()).toISOString() };
    await this.store.put(key(id), event);
    await this.store.put("next", id + 1);
    await this.prune(id);
    return { accepted: true, event };
  }

  private async prune(latest: number): Promise<void> {
    if (latest > RING_MAX) await this.store.delete(key(latest - RING_MAX));
    const oldest = await this.store.list<OpsEvent>({ prefix: "e:", limit: 3 });
    const stale: string[] = [];
    for (const [k, e] of oldest) if (this.now() - Date.parse(e.ts) > MAX_AGE_MS) stale.push(k);
    if (stale.length) await this.store.delete(stale);
  }

  async delivered(): Promise<number> {
    return (await this.store.get<number>("delivered")) ?? 0;
  }

  async markDelivered(id: number): Promise<void> {
    if (id > (await this.delivered())) await this.store.put("delivered", id);
  }

  // since = "auto" replays what no listener has received yet; a number replays after that id.
  async replay(since: string | null): Promise<OpsEvent[]> {
    const from = since && /^\d+$/.test(since) ? Number(since) : await this.delivered();
    const rows = await this.store.list<OpsEvent>({ prefix: "e:", start: key(from + 1), limit: RING_MAX });
    return [...rows.values()];
  }
}

export { frame };
