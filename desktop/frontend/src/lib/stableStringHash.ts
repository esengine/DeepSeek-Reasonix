import { contentRevision } from "./contentRevision";

// Small deterministic hash for UI keys; collisions are still namespaced by the
// source segment id and this avoids shipping a crypto implementation.
export function stableStringHash(value: string): string {
  return contentRevision(value).toString(36);
}
