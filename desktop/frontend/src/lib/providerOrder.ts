import { useEffect, useState } from "react";

export const PROVIDER_ORDER_STORAGE_KEY = "reasonix-provider-order-v1";
const PROVIDER_ORDER_CHANGED = "reasonix:provider-order-changed";

type ReadableStorage = Pick<Storage, "getItem">;
type WritableStorage = Pick<Storage, "setItem">;

function browserStorage(): Storage | undefined {
  try { return typeof localStorage === "undefined" ? undefined : localStorage; }
  catch { return undefined; }
}

export function normalizeProviderOrder(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((item): item is string => typeof item === "string" && item.trim().length > 0).map((item) => item.trim()))];
}

export function readProviderOrder(storage: ReadableStorage | undefined = browserStorage()): string[] {
  if (!storage) return [];
  try {
    const raw = storage.getItem(PROVIDER_ORDER_STORAGE_KEY);
    return raw ? normalizeProviderOrder(JSON.parse(raw)) : [];
  } catch { return []; }
}

export function orderByProvider<T>(items: readonly T[], providerID: (item: T) => string, order: readonly string[]): T[] {
  const ranks = new Map(order.map((id, index) => [id, index]));
  return items.map((item, index) => ({ item, index })).sort((a, b) =>
    (ranks.get(providerID(a.item)) ?? Infinity) - (ranks.get(providerID(b.item)) ?? Infinity) || a.index - b.index,
  ).map(({ item }) => item);
}

// Move among visible connections while retaining preferences for providers
// that are absent from this workspace's current catalog.
export function moveProvider(order: readonly string[], visibleIDs: readonly string[], id: string, direction: -1 | 1): string[] | null {
  const visible = orderByProvider([...new Set(visibleIDs)], (item) => item, order);
  const index = visible.indexOf(id);
  const neighbor = visible[index + direction];
  if (index < 0 || !neighbor) return null;
  const all = [...normalizeProviderOrder(order), ...visible.filter((item) => !order.includes(item))];
  const from = all.indexOf(id);
  const to = all.indexOf(neighbor);
  [all[from], all[to]] = [all[to], all[from]];
  return all;
}

export function writeProviderOrder(order: readonly string[], storage: WritableStorage | undefined = browserStorage()): boolean {
  if (!storage) return false;
  try {
    storage.setItem(PROVIDER_ORDER_STORAGE_KEY, JSON.stringify(normalizeProviderOrder(order)));
    if (typeof window !== "undefined") window.dispatchEvent(new window.Event(PROVIDER_ORDER_CHANGED));
    return true;
  } catch { return false; }
}

export function useProviderOrder(): string[] {
  const [order, setOrder] = useState(readProviderOrder);
  useEffect(() => {
    const refresh = () => setOrder(readProviderOrder());
    const onStorage = (event: StorageEvent) => {
      if (event.key === PROVIDER_ORDER_STORAGE_KEY || event.key === null) refresh();
    };
    window.addEventListener(PROVIDER_ORDER_CHANGED, refresh);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener(PROVIDER_ORDER_CHANGED, refresh);
      window.removeEventListener("storage", onStorage);
    };
  }, []);
  return order;
}
