/** Branching primitive separate from submit_plan; throws ChoiceRequestedError so the TUI can mount a picker and the model stops. */

import { pauseGate } from "../core/pause-gate.js";
import type { ToolRegistry } from "../tools.js";

export interface ChoiceOption {
  id: string;
  title: string;
  summary?: string;
}

export class ChoiceRequestedError extends Error {
  readonly question: string;
  readonly options: ChoiceOption[];
  readonly allowCustom: boolean;
  constructor(question: string, options: ChoiceOption[], allowCustom: boolean) {
    super(
      "ChoiceRequestedError: choice submitted. STOP calling tools now — the TUI has shown the options to the user. Wait for their next message; it will either be 'user picked <id>' (carry on with that branch), 'user answered: <text>' (custom free-form reply; read and proceed), or 'user cancelled the choice' (drop the question and ask what they want instead). Don't call any tools in the meantime.",
    );
    this.name = "ChoiceRequestedError";
    this.question = question;
    this.options = options;
    this.allowCustom = allowCustom;
  }

  toToolResult(): {
    error: string;
    question: string;
    options: ChoiceOption[];
    allowCustom: boolean;
  } {
    return {
      error: `${this.name}: ${this.message}`,
      question: this.question,
      options: this.options,
      allowCustom: this.allowCustom,
    };
  }
}

export interface ChoiceToolOptions {
  onChoiceRequested?: (question: string, options: ChoiceOption[]) => void;
}

function sanitizeOptions(raw: unknown): ChoiceOption[] {
  if (!Array.isArray(raw)) return [];
  const out: ChoiceOption[] = [];
  const seen = new Set<string>();
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const e = entry as Record<string, unknown>;
    const id = typeof e.id === "string" ? e.id.trim() : "";
    const title = typeof e.title === "string" ? e.title.trim() : "";
    if (!id || !title) continue;
    if (seen.has(id)) continue;
    seen.add(id);
    const summary = typeof e.summary === "string" ? e.summary.trim() || undefined : undefined;
    const opt: ChoiceOption = { id, title };
    if (summary) opt.summary = summary;
    out.push(opt);
  }
  return out;
}

export function registerChoiceTool(
  registry: ToolRegistry,
  opts: ChoiceToolOptions = {},
): ToolRegistry {
  registry.register({
    name: "ask_choice",
    description:
      "Render an arrow-key picker with 2–6 alternatives. Use when the user is supposed to pick — never enumerate choices as prose. Skip when one option is clearly best (just do it) or a free-form text answer fits. Max 6 options; set `allowCustom:true` when their real answer might not fit.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        question: {
          type: "string",
          description:
            "One-sentence question. Don't repeat the options here — the picker renders them.",
        },
        options: {
          type: "array",
          description: "2–6 alternatives. Each: stable id + short title; summary optional.",
          items: {
            type: "object",
            properties: {
              id: { type: "string", description: "Stable id (A, B, C or option-1)." },
              title: { type: "string", description: "One-line label." },
              summary: {
                type: "string",
                description: "Optional dimmed second line, ≤80 chars.",
              },
            },
            required: ["id", "title"],
          },
        },
        allowCustom: {
          type: "boolean",
          description: "Shows a 'type my own answer' escape hatch. Default false.",
        },
      },
      required: ["question", "options"],
    },
    fn: async (args: { question: string; options: unknown; allowCustom?: boolean }, ctx) => {
      const question = (args?.question ?? "").trim();
      if (!question) {
        throw new Error(
          "ask_choice: question is required — write one sentence explaining the decision.",
        );
      }
      const options = sanitizeOptions(args?.options);
      if (options.length < 2) {
        throw new Error(
          "ask_choice: need at least 2 well-formed options (each with a non-empty id and title). If you just need a text answer, ask the user in plain assistant text instead.",
        );
      }
      if (options.length > 6) {
        throw new Error(
          "ask_choice: too many options (max 6). If you really have this many branches, split into two sequential ask_choice calls or narrow down first.",
        );
      }
      const allowCustom = args?.allowCustom === true;
      opts.onChoiceRequested?.(question, options);
      // Block until the user picks an option, types custom text, or cancels
      const verdict = await (ctx?.confirmationGate ?? pauseGate).ask({
        kind: "choice",
        payload: { question, options, allowCustom },
      });
      if (verdict.type === "pick") return `user picked: ${verdict.optionId}`;
      if (verdict.type === "text") return `user answered: ${verdict.text}`;
      return "user cancelled the choice";
    },
  });
  return registry;
}
