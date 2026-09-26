import { useEffect, useState } from "react";

export const PROVIDER_ORDER_KEY = "rx-provider-order-v1";
const listeners = new Set<() => void>();

export function normalizeProviderOrder(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((item): item is string => typeof item === "string" && item.trim() !== "").map((item) => item.trim()))];
}

export function readProviderOrder(): string[] {
  try {
    return normalizeProviderOrder(JSON.parse(localStorage.getItem(PROVIDER_ORDER_KEY) ?? "[]"));
  } catch {
    return [];
  }
}

export function orderAccounts<T extends { key: string }>(accounts: readonly T[], order: readonly string[]): T[] {
  const rank = new Map(order.map((key, index) => [key, index]));
  return accounts.map((account, index) => ({ account, index }))
    .sort((a, b) => (rank.get(a.account.key) ?? Infinity) - (rank.get(b.account.key) ?? Infinity) || a.index - b.index)
    .map(({ account }) => account);
}

// Keep saved positions for accounts absent from the current model catalog.
export function moveAccount(order: readonly string[], visible: readonly string[], key: string, direction: -1 | 1): string[] | null {
  const shown = orderAccounts([...new Set(visible)].map((id) => ({ key: id })), order).map((item) => item.key);
  const index = shown.indexOf(key);
  const neighbor = shown[index + direction];
  if (index < 0 || !neighbor) return null;
  const all = [...normalizeProviderOrder(order), ...shown.filter((id) => !order.includes(id))];
  const from = all.indexOf(key);
  const to = all.indexOf(neighbor);
  [all[from], all[to]] = [all[to], all[from]];
  return all;
}

export function writeProviderOrder(order: readonly string[]): boolean {
  try {
    localStorage.setItem(PROVIDER_ORDER_KEY, JSON.stringify(normalizeProviderOrder(order)));
    listeners.forEach((notify) => notify());
    return true;
  } catch {
    return false;
  }
}

export function useProviderOrder(): string[] {
  const [order, setOrder] = useState(readProviderOrder);
  useEffect(() => {
    const refresh = () => setOrder(readProviderOrder());
    const onStorage = (event: StorageEvent) => {
      if (event.key === PROVIDER_ORDER_KEY || event.key === null) refresh();
    };
    listeners.add(refresh);
    window.addEventListener("storage", onStorage);
    return () => {
      listeners.delete(refresh);
      window.removeEventListener("storage", onStorage);
    };
  }, []);
  return order;
}
