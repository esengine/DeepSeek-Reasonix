import type { AgentPort } from "../port/port";

export const STORAGE_TALLY_TTL = 10 * 60 * 1000;
export const STORAGE_TALLY_DELAY = 1500;

type Taken = { at: number; total: number } | { walk: Promise<number> };

const taken = new WeakMap<object, Taken>();

export async function storageTotal(port: Pick<AgentPort, "storage">, now: () => number = Date.now): Promise<number> {
  const held = taken.get(port);
  if (held && "walk" in held) return held.walk;
  if (held && now() - held.at < STORAGE_TALLY_TTL) return held.total;
  const walk = port.storage().then((st) => st.roots.reduce((n, r) => n + (r.bytes || 0), 0));
  taken.set(port, { walk });
  try {
    const total = await walk;
    taken.set(port, { at: now(), total });
    return total;
  } catch (e) {
    taken.delete(port);
    throw e;
  }
}

export function cachedStorageTotal(port: object, now: () => number = Date.now): number | undefined {
  const held = taken.get(port);
  return held && "at" in held && now() - held.at < STORAGE_TALLY_TTL ? held.total : undefined;
}
