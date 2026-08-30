import { lazy, Suspense } from "react";
import type { SearchSource } from "../lib/searchSources";

const SearchSourcesPanel = lazy(() => import("./SearchSourcesPanel").then((module) => ({ default: module.SearchSourcesPanel })));

// Backwards-compatible export for callers that still use the old name. The
// display implementation is now the structured, collapsed sources panel.
export function SearchFootnotes({ sources }: { sources?: SearchSource[] }) {
  return (
    <Suspense fallback={null}>
      <SearchSourcesPanel sources={sources} />
    </Suspense>
  );
}

export const hasSearchFootnotes = (sources?: SearchSource[]) => Boolean(sources?.length);
