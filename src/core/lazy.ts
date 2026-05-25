/** Memoize an async loader so the first call kicks off work and every later call returns the same promise. */
export function lazy<T>(load: () => Promise<T>): () => Promise<T> {
  let pending: Promise<T> | null = null;
  return () => {
    if (!pending) pending = load();
    return pending;
  };
}
