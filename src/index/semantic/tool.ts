import type { ToolRegistry } from "../../tools.js";
import { indexCompatible, indexExists, querySemantic } from "./builder.js";
import type { SearchHit } from "./store.js";

type SemanticToolOptions = {
  provider?: "ollama" | "openai-compat";
  baseUrl?: string;
  apiKey?: string;
  model?: string;
  extraBody?: Record<string, unknown>;
  timeoutMs?: number;
  root: string;
  defaultTopK?: number;
  defaultMinScore?: number;
};

export async function registerSemanticSearchTool(
  registry: ToolRegistry,
  opts: SemanticToolOptions,
): Promise<boolean> {
  if (!(await indexCompatible(opts.root, { provider: opts.provider, model: opts.model })))
    return false;
  const defaultTopK = opts.defaultTopK ?? 8;
  const defaultMinScore = opts.defaultMinScore ?? 0.3;

  registry.register({
    name: "semantic_search",
    description:
      "FIRST CHOICE for descriptive queries. Use this BEFORE search_content (grep) when the user describes WHAT code does ('where do we handle X', 'which file owns Y', 'how does Z work', 'find the logic that …'). Returns ranked snippets ordered by semantic relevance — finds the right file even when your description shares no words with the code. Falls back to search_content / search_files only for: exact identifiers, regex patterns, or counting occurrences of a known token. If your first instinct is grep on a paraphrased question, you are wrong — try semantic_search first.",
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        query: {
          type: "string",
          description:
            "Natural-language description, phrased as a question or noun phrase: 'where do we validate the session cookie?' / 'retry backoff logic' / 'code that prevents user changes from immediately landing on disk'. Do NOT pass exact identifiers — those are search_content's job.",
        },
        topK: {
          type: "integer",
          description: `Number of snippets to return (1..16). Default ${defaultTopK}.`,
        },
        minScore: {
          type: "number",
          description: `Drop snippets with cosine score below this (0..1). Default ${defaultMinScore}. Raise for stricter matches; lower if the index is small.`,
        },
      },
      required: ["query"],
    },
    fn: async (args: { query: string; topK?: number; minScore?: number }, ctx) => {
      const hits = await querySemantic(opts.root, args.query, {
        topK: args.topK ?? defaultTopK,
        minScore: args.minScore ?? defaultMinScore,
        provider: opts.provider,
        baseUrl: opts.baseUrl,
        apiKey: opts.apiKey,
        model: opts.model,
        extraBody: opts.extraBody,
        signal: ctx?.signal,
      });
      if (hits === null) {
        return "No semantic index found for this project. Run `reasonix index` to build one.";
      }
      if (hits.length === 0) {
        return `query: ${args.query}\n\nno matches above the score threshold (${args.minScore ?? defaultMinScore}).`;
      }
      return formatHits(args.query, hits);
    },
  });
  return true;
}

export function formatHits(query: string, hits: readonly SearchHit[]): string {
  const lines: string[] = [`query: ${query}`, `\nresults (${hits.length}):`];
  hits.forEach((h, i) => {
    const { entry, score } = h;
    lines.push(
      `\n${i + 1}. ${entry.path}:${entry.startLine}-${entry.endLine}  (score ${score.toFixed(3)})`,
    );
    // Cap each snippet so a 60-line chunk doesn't dominate the
    // model's context. The full chunk is still discoverable via
    // read_file once the model picks the most relevant hit.
    const preview = entry.text.split("\n").slice(0, 8).join("\n");
    lines.push(indentBlock(preview, "   "));
    if (entry.text.split("\n").length > 8) {
      lines.push(
        `   …(${entry.text.split("\n").length - 8} more lines — read_file ${entry.path}:${entry.startLine} for the full chunk)`,
      );
    }
  });
  return lines.join("\n");
}

function indentBlock(text: string, prefix: string): string {
  return text
    .split("\n")
    .map((l) => prefix + l)
    .join("\n");
}

/** Silent: register if index exists, else skip — no Ollama probe, no setup prompt. */
export async function bootstrapSemanticSearchInCodeMode(
  registry: ToolRegistry,
  rootDir: string,
  opts: Omit<SemanticToolOptions, "root" | "defaultTopK" | "defaultMinScore"> = {},
): Promise<{ enabled: boolean }> {
  if (await indexCompatible(rootDir, { provider: opts.provider, model: opts.model })) {
    await registerSemanticSearchTool(registry, { ...opts, root: rootDir });
    return { enabled: true };
  }
  return { enabled: false };
}
