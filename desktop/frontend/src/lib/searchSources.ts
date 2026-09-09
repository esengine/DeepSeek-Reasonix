export type SearchSource = {
  title?: string;
  url?: string;
};

export interface HistoryServerSearch {
  id: string;
  query?: string;
  sources_status?: "available" | "not_provided";
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
  if (output.trimStart().startsWith("{")) {
    try {
      const result: unknown = JSON.parse(output);
      if (result && typeof result === "object" && "sources" in result && Array.isArray(result.sources)) {
        return result.sources.flatMap((source: unknown): SearchSource[] => {
          if (!source || typeof source !== "object" || !("url" in source) || typeof source.url !== "string" || !isSafeHttpUrl(source.url)) return [];
          return [{ url: source.url, title: "title" in source && typeof source.title === "string" ? source.title : undefined }];
        });
      }
    } catch {
      // Old search output is plain text; preserve its existing parser.
    }
  }
  const lines = output.split("\n").map((line) => line.trim()).filter(Boolean);
  const out: SearchSource[] = [];
  for (const line of lines) {
    // Tolerate the footnote-markdown shape (`- **title**` / `<url>`) as well:
    // if a degraded plain-text dump ever reaches this parser (#8900), sources
    // still resolve into cards/footnotes instead of leaking raw markup.
    const urlMatch = /^<?(https?:\/\/[^>\s]+)>?$/i.exec(line);
    if (urlMatch) {
      const last = out[out.length - 1];
      if (last && !last.url) last.url = urlMatch[1];
      else out.push({ url: urlMatch[1] });
      continue;
    }
    const titleMatch = /^[-*]\s+\*\*(.+)\*\*$/.exec(line);
    out.push({ title: titleMatch?.[1] ?? line });
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

// Only explicit structured metadata carries availability; never infer it from prose.
export function searchOutputMetadata(output?: string): { status?: "available" | "not_provided"; summary?: string } {
  if (!output) return {};
  try {
    const value = JSON.parse(output);
    if (!value || typeof value !== "object") return {};
    return { status: value.sources_status === "available" || value.sources_status === "not_provided" ? value.sources_status : undefined,
      summary: typeof value.summary === "string" ? value.summary : undefined };
  } catch { return {}; }
}
