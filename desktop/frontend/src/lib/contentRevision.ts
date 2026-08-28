/** Deterministic UTF-16 content fingerprint used by transcript caches. */
export function contentRevision(text: string, hash = 0x811c9dc5): number {
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

/** Geometry fingerprint including field boundaries and UTF-16 lengths. */
export function hashGeometryParts(parts: readonly string[]): string {
  let hash = 2166136261;
  for (const part of parts) {
    hash = Math.imul(contentRevision(part, hash) ^ 31, 16777619);
  }
  return `${hash >>> 0}:${parts.map((part) => part.length).join(",")}`;
}
