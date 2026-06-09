export function mergedFetchedProviderModels(current: string[], fetched: string[], options: { preserveCurated?: boolean } = {}): string[] {
  const saved = uniqueStrings(current);
  if (options.preserveCurated && saved.length > 0) return saved;
  return uniqueStrings([...saved, ...fetched]);
}

export function providerDefaultModel(currentDefault: string, models: string[]): string {
  return currentDefault && models.includes(currentDefault) ? currentDefault : models[0] ?? "";
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    out.push(model);
  }
  return out;
}
