const draftIDPattern = /draft:([0-9a-fA-F]{32})/g;

export function draftIDsFromSubmit(input: string): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  draftIDPattern.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = draftIDPattern.exec(input))) {
    const id = match[1].toLowerCase();
    if (seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}
