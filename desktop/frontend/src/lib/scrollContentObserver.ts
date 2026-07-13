export function observeScrollContentSize(element: HTMLElement, onChange: () => void): () => void {
  if (typeof ResizeObserver === "undefined") return () => {};

  const observed = new Set<Element>();
  const resizeObserver = new ResizeObserver(onChange);
  const syncChildren = () => {
    const current = new Set(Array.from(element.children));
    for (const child of observed) {
      if (current.has(child)) continue;
      resizeObserver.unobserve(child);
      observed.delete(child);
    }
    for (const child of current) {
      if (observed.has(child)) continue;
      resizeObserver.observe(child);
      observed.add(child);
    }
  };

  syncChildren();
  const mutationObserver = typeof MutationObserver === "undefined"
    ? null
    : new MutationObserver(() => {
        syncChildren();
        onChange();
      });
  mutationObserver?.observe(element, { childList: true });

  return () => {
    mutationObserver?.disconnect();
    resizeObserver.disconnect();
    observed.clear();
  };
}
