/** Writes are eager but the prefix is NOT re-loaded mid-session — keeps prompt-cache stable. */

import {
  type MemoryScope,
  MemoryStore,
  type MemoryType,
  sanitizeMemoryName,
} from "../memory/user.js";
import type { ToolRegistry } from "../tools.js";

export interface MemoryToolsOptions {
  /** Sandbox root for the `project` scope. Omit for chat mode. */
  projectRoot?: string;
  /** Override `~/.reasonix` (tests). */
  homeDir?: string;
}

export function registerMemoryTools(
  registry: ToolRegistry,
  opts: MemoryToolsOptions = {},
): ToolRegistry {
  const store = new MemoryStore({ homeDir: opts.homeDir, projectRoot: opts.projectRoot });
  const hasProject = store.hasProjectScope();

  registry.register({
    name: "remember",
    description:
      "Save a memory for future sessions. Use when the user states a preference, corrects your approach, shares a non-obvious fact about this project, or explicitly asks you to remember something. Don't remember transient task state — only things worth recalling next session. The memory is written now but won't re-load into the system prompt until the next `/new` or launch.",
    parameters: {
      type: "object",
      properties: {
        type: {
          type: "string",
          enum: ["user", "feedback", "project", "reference"],
          description:
            "'user' = role/skills/prefs; 'feedback' = corrections or confirmed approaches; 'project' = facts/decisions about the current work; 'reference' = pointers to external systems the user uses.",
        },
        scope: {
          type: "string",
          enum: ["global", "project"],
          description:
            "'global' = applies across every project (preferences, tooling); 'project' = scoped to the current sandbox (decisions, local facts). Only available in `reasonix code`.",
        },
        name: {
          type: "string",
          description:
            "filename-safe identifier, 3-40 chars, alnum + _ - . (no path separators, no leading dot).",
        },
        description: {
          type: "string",
          description: "One-line summary shown in MEMORY.md (under ~150 chars).",
        },
        content: {
          type: "string",
          description:
            "Full memory body in markdown. For feedback/project types, structure as: rule/fact, then **Why:** line, then **How to apply:** line.",
        },
      },
      required: ["type", "scope", "name", "description", "content"],
    },
    fn: async (args: {
      type: MemoryType;
      scope: MemoryScope;
      name: string;
      description: string;
      content: string;
    }) => {
      if (args.scope === "project" && !hasProject) {
        return JSON.stringify({
          error:
            "scope='project' is unavailable in this session (no sandbox root). Retry with scope='global', or ask the user to switch to `reasonix code` for project-scoped memory.",
        });
      }
      try {
        const path = store.write({
          name: args.name,
          type: args.type,
          scope: args.scope,
          description: args.description,
          body: args.content,
        });
        const key = sanitizeMemoryName(args.name);
        // The return text is load-bearing: it's the ONLY thing keeping
        // the fact visible within the current session, because the
        // prefix isn't re-hashed mid-session (Pillar 1). R1 reads this
        // on its next turn — the wording is deliberately imperative so
        // it doesn't get ignored in favor of explore-first behavior.
        return [
          `✓ REMEMBERED (${args.scope}/${key}): ${args.description}`,
          "",
          "TREAT THIS AS ESTABLISHED FACT for the rest of this session.",
          "The user just told you — don't re-explore the filesystem to re-derive it.",
          `(Saved to ${path}; pins into the system prompt on next /new or launch.)`,
        ].join("\n");
      } catch (err) {
        return JSON.stringify({ error: `remember failed: ${(err as Error).message}` });
      }
    },
  });

  registry.register({
    name: "forget",
    description:
      "Delete a memory file and remove it from MEMORY.md. Use when the user explicitly asks to forget something, or when a previously-remembered fact has become wrong. Irreversible — no tombstone.",
    parameters: {
      type: "object",
      properties: {
        name: { type: "string", description: "Memory name (the identifier used in `remember`)." },
        scope: { type: "string", enum: ["global", "project"] },
      },
      required: ["name", "scope"],
    },
    fn: async (args: { name: string; scope: MemoryScope }) => {
      if (args.scope === "project" && !hasProject) {
        return JSON.stringify({
          error: "scope='project' is unavailable in this session (no sandbox root).",
        });
      }
      try {
        const existed = store.delete(args.scope, args.name);
        return existed
          ? `forgot (${args.scope}/${sanitizeMemoryName(args.name)}). Re-load on next /new or launch.`
          : `no such memory: ${args.scope}/${args.name} (nothing to forget).`;
      } catch (err) {
        return JSON.stringify({ error: `forget failed: ${(err as Error).message}` });
      }
    },
  });

  registry.register({
    name: "recall_memory",
    description:
      "Read the full body of a memory file when its MEMORY.md one-liner (already in the system prompt) isn't enough detail. Most of the time the index suffices — only call this when the user's question genuinely requires the full context.",
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        name: { type: "string" },
        scope: { type: "string", enum: ["global", "project"] },
      },
      required: ["name", "scope"],
    },
    fn: async (args: { name: string; scope: MemoryScope }) => {
      if (args.scope === "project" && !hasProject) {
        return JSON.stringify({
          error: "scope='project' is unavailable in this session (no sandbox root).",
        });
      }
      try {
        const entry = store.read(args.scope, args.name);
        return [
          `# ${entry.name}  (${entry.scope}/${entry.type}, created ${entry.createdAt || "?"})`,
          entry.description ? `> ${entry.description}` : "",
          "",
          entry.body,
        ]
          .filter(Boolean)
          .join("\n");
      } catch (err) {
        return JSON.stringify({ error: `recall failed: ${(err as Error).message}` });
      }
    },
  });

  return registry;
}
