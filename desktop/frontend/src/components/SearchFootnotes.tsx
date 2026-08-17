import { formatSearchFootnotesMarkdown } from "../lib/searchSources";
import type { SearchSource } from "../lib/searchSources";
import { Markdown } from "./Markdown";

export function SearchFootnotes({ sources }: { sources?: SearchSource[] }) {
  const footnotes = formatSearchFootnotesMarkdown(sources ?? []);
  if (!footnotes) return null;
  return (
    <div className="msg-search-sources">
      <Markdown text={footnotes} />
    </div>
  );
}

export function hasSearchFootnotes(sources?: SearchSource[]): boolean {
  return formatSearchFootnotesMarkdown(sources ?? []) !== "";
}
