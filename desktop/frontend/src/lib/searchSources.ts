export type SearchSource = {
  title?: string;
  url?: string;
};

export interface HistoryServerSearch {
  id: string;
  query?: string;
  results?: { title?: string; url?: string }[];
}

export function isSafeHttpUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

function sourceKey(source: SearchSource): string {
  return `${source.url ?? ""}\n${source.title ?? ""}`;
}

export function mergeSearchSources(dst: SearchSource[] | undefined, add: SearchSource[]): SearchSource[] {
  const out = dst ? dst.slice() : [];
  const seen = new Set(out.map(sourceKey));
  for (const hit of add) {
    if (!hit.title && !hit.url) continue;
    const key = sourceKey(hit);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ title: hit.title, url: hit.url });
  }
  return out;
}

export function searchSourcesFromHistory(searches: { results?: { title?: string; url?: string }[] }[] | undefined): SearchSource[] {
  const add: SearchSource[] = [];
  for (const search of searches ?? []) {
    for (const hit of search.results ?? []) {
      if (hit.title || hit.url) add.push({ title: hit.title, url: hit.url });
    }
  }
  return mergeSearchSources(undefined, add);
}

export function parseSearchSources(output: string): SearchSource[] {
  const lines = output.split("\n").map((line) => line.trim()).filter(Boolean);
  const out: SearchSource[] = [];
  for (const line of lines) {
    if (/^https?:\/\//i.test(line)) {
      const last = out[out.length - 1];
      if (last && !last.url) last.url = line;
      else out.push({ url: line });
      continue;
    }
    out.push({ title: line });
  }
  return out;
}

/** Same title + autolink list the old answer dump used, rendered after the reply. */
export function formatSearchFootnotesMarkdown(sources: SearchSource[]): string {
  const lines: string[] = [];
  for (const source of sources) {
    if (!source.title && !source.url) continue;
    lines.push(`- **${source.title ?? ""}**`);
    if (source.url && isSafeHttpUrl(source.url)) lines.push(`  <${source.url}>`);
  }
  return lines.length > 0 ? `\n${lines.join("\n")}\n` : "";
}
