/** Preserve transcript on failed hydrate instead of painting a successful empty session. */
export function applyHydrateErrorState<
  TItem,
  S extends {
    items: TItem[];
    hydratePlaceholderItems?: TItem[];
    hydrateHistoryLoaded?: boolean;
  },
>(s: S, reason: string, error: string): S & {
  hydrating: false;
  hydrateReason: string;
  hydrateError: string;
  hydrateHistoryLoaded?: boolean;
  hydratePlaceholderItems: undefined;
} {
  const keptItems = s.items.length > 0
    ? s.items
    : (s.hydratePlaceholderItems?.length ? s.hydratePlaceholderItems : s.items);
  return {
    ...s,
    items: keptItems,
    hydrating: false,
    hydrateReason: reason,
    hydrateError: error,
    hydrateHistoryLoaded: s.hydrateHistoryLoaded || keptItems.length > 0 || undefined,
    hydratePlaceholderItems: undefined,
  };
}

export function hydratePlaceholderItems<TItem>(
  optionsItems: TItem[] | undefined,
): TItem[] | undefined {
  return optionsItems?.length ? optionsItems : undefined;
}
