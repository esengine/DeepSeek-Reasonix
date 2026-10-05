import { useSyncExternalStore } from "react";
import { normalizeProviderOrder, onProviderOrderChange, readProviderOrder } from "./prefs";

export { normalizeProviderOrder, PROVIDER_ORDER_KEY, readProviderOrder, writeProviderOrder } from "./prefs";

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

export function useProviderOrder(): readonly string[] {
  return useSyncExternalStore(onProviderOrderChange, readProviderOrder, readProviderOrder);
}
