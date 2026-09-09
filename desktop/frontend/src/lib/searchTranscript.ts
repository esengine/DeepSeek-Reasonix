import { mergeSearchSources, parseSearchSources, searchSourcesFromHistory, searchOutputMetadata, type SearchSource } from "./searchSources";
import type { Item } from "./useController";

type SearchState = {
  currentAssistant?: string;
  pendingSearchSources?: SearchSource[];
  items: Item[];
};

export function historySearchCards(
  searches: { id?: string; query?: string; sources_status?: "available" | "not_provided"; results?: { title?: string; url?: string }[] }[] | undefined,
): Extract<Item, { kind: "tool" }>[] {
  const cards: Extract<Item, { kind: "tool" }>[] = [];
  for (const search of searches ?? []) {
    if (!search.id) continue;
    const lines = (search.results ?? []).flatMap((hit) => [hit.title, hit.url].filter(Boolean));
    cards.push({
      kind: "tool",
      id: search.id,
      name: "web_search",
      args: search.query ? JSON.stringify({ query: search.query }) : "",
      readOnly: true,
      status: "done",
      searchSourcesStatus: search.sources_status,
      searchSources: (search.results ?? []).map((hit) => ({ title: hit.title, url: hit.url })),
      output: lines.join("\n"),
    });
  }
  return cards;
}

export function historySearchSources(
  searches: { results?: { title?: string; url?: string }[] }[] | undefined,
): SearchSource[] | undefined {
  const sources = searchSourcesFromHistory(searches);
  return sources.length > 0 ? sources : undefined;
}

export function historySearchAndAnswer(
  id: string,
  m: { content: string; reasoning?: string; workDurationMs?: number; memoryCitations?: Extract<Item, { kind: "assistant" }>["memoryCitations"]; serverSearch?: { id?: string; query?: string; sources_status?: "available" | "not_provided"; results?: { title?: string; url?: string }[] }[] },
): Item[] {
  const out: Item[] = historySearchCards(m.serverSearch);
  const searchSources = historySearchSources(m.serverSearch);
  if (m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "" || searchSources) {
    out.push({
      kind: "assistant",
      id,
      text: m.content,
      reasoning: m.reasoning ?? "",
      streaming: false,
      workDurationMs: m.workDurationMs,
      memoryCitations: m.memoryCitations,
      searchSources,
    });
  }
  return out;
}

export function attachSearchSources<T extends SearchState>(s: T, sources: SearchSource[]): T {
  if (sources.length === 0) return s;
  const currentAssistant = s.currentAssistant && s.items.some(
    (it) => it.kind === "assistant" && it.id === s.currentAssistant,
  )
    ? s.currentAssistant
    : undefined;
  if (!currentAssistant) {
    return { ...s, pendingSearchSources: mergeSearchSources(s.pendingSearchSources, sources) };
  }
  return {
    ...s,
    items: s.items.map((it) =>
      it.kind === "assistant" && it.id === currentAssistant
        ? { ...it, searchSources: mergeSearchSources(it.searchSources, sources) }
        : it,
    ),
  };
}

export function isBatchedReadOnlyTool(name: string, readOnly: boolean): boolean {
  return readOnly && name !== "todo_write" && name !== "web_search";
}

export function attachWebSearchOutput<T extends SearchState>(s: T, name: string, output?: string, err?: string, toolId?: string): T {
  if (name !== "web_search" || !output || err) return s;
  const sources = parseSearchSources(output);
  const withTool = toolId
    ? {
        ...s,
        items: s.items.map((it) =>
          it.kind === "tool" && it.id === toolId
            ? { ...it, searchSources: mergeSearchSources(it.searchSources, sources), searchSourcesStatus: searchOutputMetadata(output).status, searchSummary: searchOutputMetadata(output).summary }
            : it,
        ),
      }
    : s;
  return attachSearchSources(withTool, sources);
}
