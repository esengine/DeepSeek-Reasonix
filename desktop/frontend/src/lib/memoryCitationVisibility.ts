import type { MemoryCitation } from "./types";

const compilerCitationKinds = new Set(["compiler_reference", "constraint", "risk_note"]);

export function visibleTranscriptMemoryCitations(citations?: MemoryCitation[]): MemoryCitation[] {
  return (citations ?? []).filter((citation) => !isMemoryCompilerCitation(citation));
}

function isMemoryCompilerCitation(citation: MemoryCitation): boolean {
  const kind = (citation.kind ?? "").trim();
  if (!compilerCitationKinds.has(kind)) return false;
  const source = (citation.source || citation.id || "").trim().toLowerCase();
  return source === "" || source === "memory v5";
}
