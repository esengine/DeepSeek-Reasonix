// The listing the kernel writes for the model: "- **title**" and, indented under
// it, "<url>". Only a run of exactly that gets lifted out of the prose — reading
// a paragraph as a result list is worse than leaving the list in place.
const SEARCH_TITLE = /^-\s+\*\*.+\*\*\s*$/;
const SEARCH_URL = /^\s+<\S+>\s*$/;

export function splitProviderSearch(content: string): { text: string; search?: boolean }[] {
  const lines = content.split("\n");
  const blocks: [number, number][] = [];
  for (let i = 0; i < lines.length; ) {
    if (!SEARCH_TITLE.test(lines[i])) {
      i++;
      continue;
    }
    let end = i;
    let urls = 0;
    while (end < lines.length && (SEARCH_TITLE.test(lines[end]) || SEARCH_URL.test(lines[end]))) {
      if (SEARCH_URL.test(lines[end])) urls++;
      end++;
    }
    // A run with no source is the model's own bolded list — an answer written as
    // "- **厄瓜多尔总统将访华**" reads exactly like a result title.
    if (urls > 0) blocks.push([i, end]);
    i = end > i ? end : i + 1;
  }
  const parts: { text: string; search?: boolean }[] = [];
  const push = (from: number, to: number, search: boolean) => {
    const text = lines.slice(from, to).join("\n").trim();
    if (text) parts.push(search ? { text, search: true } : { text });
  };
  let at = 0;
  for (const [from, to] of blocks) {
    push(at, from, false);
    push(from, to, true);
    at = to;
  }
  push(at, lines.length, false);
  return parts;
}
