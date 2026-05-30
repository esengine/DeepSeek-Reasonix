import type { Card } from "./state/cards.js";

export type CopyHistoryMode =
  | { kind: "latest-assistant" }
  | { kind: "all" }
  | { kind: "last"; count: number };

export interface CopyHistorySelection {
  label: string;
  text: string;
}

export function parseCopyHistoryArgs(args: readonly string[]): CopyHistoryMode | { error: string } {
  if (args.length === 0) return { kind: "latest-assistant" };
  if (args.length === 1) {
    const raw = args[0]!.toLowerCase();
    if (raw === "last" || raw === "assistant" || raw === "reply" || raw === "response") {
      return { kind: "latest-assistant" };
    }
    if (raw === "all") return { kind: "all" };
    if (/^\d+$/.test(raw)) {
      const count = Number.parseInt(raw, 10);
      if (count > 0) return { kind: "last", count };
    }
  }
  return { error: "usage" };
}

export function selectCopyHistory(
  cards: ReadonlyArray<Card>,
  mode: CopyHistoryMode = { kind: "latest-assistant" },
): CopyHistorySelection | null {
  if (mode.kind === "latest-assistant") {
    for (let i = cards.length - 1; i >= 0; i--) {
      const card = cards[i]!;
      if (card.kind !== "streaming" || !card.done) continue;
      const text = normalize(card.text);
      if (text) return { label: "last assistant response", text };
    }
    return null;
  }

  const entries = cards.flatMap(cardToCopyEntries).filter((entry) => entry.text.length > 0);
  const selected = mode.kind === "all" ? entries : entries.slice(-mode.count);
  if (selected.length === 0) return null;
  return {
    label: mode.kind === "all" ? "conversation" : `last ${selected.length} item(s)`,
    text: selected.map((entry) => `${entry.label}:\n${entry.text}`).join("\n\n"),
  };
}

interface CopyEntry {
  label: string;
  text: string;
}

function cardToCopyEntries(card: Card): CopyEntry[] {
  switch (card.kind) {
    case "user":
      return [{ label: "User", text: normalize(card.text) }];
    case "streaming":
      return [{ label: "Assistant", text: normalize(card.text) }];
    case "tool": {
      const args = card.args === undefined ? "" : formatJson(card.args);
      const body = [args ? `Args:\n${args}` : "", card.output ? `Output:\n${card.output}` : ""]
        .filter(Boolean)
        .join("\n\n");
      return [{ label: `Tool ${card.name}`, text: normalize(body) }];
    }
    case "reasoning":
      return [{ label: "Reasoning", text: normalize(card.text) }];
    case "live":
      return [
        { label: "Info", text: normalize(card.meta ? `${card.text}\n${card.meta}` : card.text) },
      ];
    case "warn":
      return [
        {
          label: "Warning",
          text: normalize([card.title, card.message, card.detail].filter(Boolean).join("\n")),
        },
      ];
    case "error":
      return [
        {
          label: "Error",
          text: normalize([card.title, card.message, card.stack].filter(Boolean).join("\n")),
        },
      ];
    case "plan":
      return [
        {
          label: "Plan",
          text: normalize(
            [card.title, ...card.steps.map((step) => `- [${step.status}] ${step.title}`)].join(
              "\n",
            ),
          ),
        },
      ];
    case "task":
      return [
        {
          label: "Task",
          text: normalize(
            [
              `${card.title} (${card.status})`,
              ...card.steps.map((step) => `- [${step.status}] ${step.title}`),
            ].join("\n"),
          ),
        },
      ];
    case "diff":
      return [
        {
          label: `Diff ${card.file}`,
          text: normalize(
            card.hunks
              .map((hunk) =>
                [
                  hunk.header,
                  ...hunk.lines.map(
                    (line) =>
                      `${line.kind === "add" ? "+" : line.kind === "del" ? "-" : " "}${line.text}`,
                  ),
                ].join("\n"),
              )
              .join("\n\n"),
          ),
        },
      ];
    case "usage":
      return [
        {
          label: "Usage",
          text: normalize(
            `turn ${card.turn}: prompt ${card.tokens.prompt}, reasoning ${card.tokens.reason}, output ${card.tokens.output}, cache ${card.cacheHit}%, cost $${card.cost}`,
          ),
        },
      ];
    case "memory":
      return [
        {
          label: "Memory",
          text: normalize(
            card.entries.map((entry) => `- ${entry.category}: ${entry.summary}`).join("\n"),
          ),
        },
      ];
    case "subagent":
      return [
        {
          label: `Subagent ${card.name}`,
          text: normalize(
            [
              `${card.task} (${card.status})`,
              ...card.children
                .flatMap(cardToCopyEntries)
                .map((entry) => `${entry.label}:\n${entry.text}`),
            ].join("\n\n"),
          ),
        },
      ];
    case "search":
      return [
        {
          label: "Search",
          text: normalize(
            [
              `query: ${card.query}`,
              ...card.hits.map((hit) => `${hit.file}:${hit.line}: ${hit.preview}`),
            ].join("\n"),
          ),
        },
      ];
    case "ctx":
      return [{ label: "Context", text: normalize(card.text) }];
    case "tip":
      return [
        {
          label: "Tip",
          text: normalize(
            [
              card.topic,
              ...card.sections.flatMap((section) => [
                section.title ? `[${section.title}]` : "",
                ...section.rows.map((row) => `${row.key}\t${row.text}`),
              ]),
              card.footer ?? "",
            ].join("\n"),
          ),
        },
      ];
    case "doctor":
      return [
        {
          label: "Doctor",
          text: normalize(
            card.checks.map((c) => `- [${c.level}] ${c.label}: ${c.detail}`).join("\n"),
          ),
        },
      ];
    case "compaction":
      return [{ label: "Compaction", text: normalize(card.summary) }];
    default:
      return [];
  }
}

function normalize(text: string): string {
  return text.trim();
}

function formatJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
