/** True when a hydrate started for loadPath/loadGen still matches live meta. */
export function hydrateIdentityCurrent(
  loadPath: string,
  loadGeneration: number | undefined,
  metaPath: string | undefined,
  metaGeneration: number | undefined,
): boolean {
  const path = loadPath.trim();
  if (path && metaPath && metaPath !== path) return false;
  if (loadGeneration !== undefined && metaGeneration !== undefined && metaGeneration !== loadGeneration) {
    return false;
  }
  return true;
}
