export function createUniqueItemIDAllocator(): (preferred: string, fallback: string) => string {
  const used = new Set<string>();
  return (preferred, fallback) => {
    const base = preferred || fallback;
    if (!used.has(base)) {
      used.add(base);
      return base;
    }
    let suffix = 1;
    while (used.has(`${base}#${suffix}`)) suffix += 1;
    const id = `${base}#${suffix}`;
    used.add(id);
    return id;
  };
}
