// Small deterministic hash for UI keys; collisions are still namespaced by the
// source segment id and this avoids shipping a crypto implementation.
export function stableStringHash(value: string): string {
  let hash = 2166136261;
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}
