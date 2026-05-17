/** runAs: inline appends the body to the parent log; subagent spawns an isolated child loop and only returns the final answer. */

import { type Skill, SkillStore } from "../skills.js";
import type { ToolRegistry } from "../tools.js";

/** Returns serialized tool-result string — dispatch path is pure pass-through. */
export type SubagentRunner = (skill: Skill, task: string, signal?: AbortSignal) => Promise<string>;

/** Fired after a successful `install_skill` write — host wires this to push a fresh `$skills` event so the desktop sidebar updates without a tab reload. */
export type SkillInstalledHook = (info: {
  name: string;
  path: string;
  scope: "project" | "global";
}) => void;

export interface SkillToolsOptions {
  /** Override `$HOME` — tests set this to a tmpdir. */
  homeDir?: string;
  projectRoot?: string;
  customSkillPaths?: readonly string[];
  /** When omitted, subagent skills error rather than silently falling back to inline (loses isolation). */
  subagentRunner?: SubagentRunner;
  /** Hide built-in skills (test-only knob; production callers leave off). */
  disableBuiltins?: boolean;
  /** Called synchronously after `install_skill` successfully writes a new skill file. */
  onSkillInstalled?: SkillInstalledHook;
}

export function registerSkillTools(
  registry: ToolRegistry,
  opts: SkillToolsOptions = {},
): ToolRegistry {
  const store = new SkillStore({
    homeDir: opts.homeDir,
    projectRoot: opts.projectRoot,
    customSkillPaths: opts.customSkillPaths,
    disableBuiltins: opts.disableBuiltins,
  });
  const subagentRunner = opts.subagentRunner;
  const onSkillInstalled = opts.onSkillInstalled;
  const hasProjectScope = store.hasProjectScope();

  registry.register({
    name: "run_skill",
    description:
      "Invoke a playbook from the Skills index pinned in the system prompt. Each entry is a self-contained instruction block. Pass `name` as the BARE skill identifier (e.g. 'explore'), NOT the `[🧬 subagent]` tag that appears after it in the index. Entries tagged `[🧬 subagent]` spawn an isolated subagent — only the final distilled answer comes back, the model's tool calls + reasoning during the run never enter your context. Plain skills are inlined: the body becomes a tool result you read and follow. For subagent skills, supply 'arguments' describing the concrete task — they'll be the only context the subagent has.",
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description:
            "Skill identifier as it appears in the pinned Skills index (e.g. 'explore', 'review', 'security-review'). Case-sensitive.",
        },
        arguments: {
          type: "string",
          description:
            "Free-form arguments the skill should act on. For inline skills: appended to the body as an 'Arguments:' line; the skill's own instructions decide how to consume them. For `[🧬 subagent]` skills: REQUIRED — becomes the entire task description the subagent receives, since it has no other context.",
        },
      },
      required: ["name"],
    },
    fn: async (args: { name?: unknown; arguments?: unknown }, ctx) => {
      const raw = typeof args.name === "string" ? args.name.trim() : "";
      if (!raw) {
        return JSON.stringify({ error: "run_skill requires a 'name' argument" });
      }
      // Defensive: The Skills index writes entries like
      // `explore [🧬 subagent]`, and models sometimes copy the
      // decoration verbatim into the `name` argument instead of just
      // the identifier. Rather than reject those calls:
      //   1. Drop any `[...]` bracketed tag (possibly containing
      //      emoji + "subagent" label).
      //   2. Find the first whitespace-delimited token whose first
      //      char is alphanumeric — that's the skill identifier,
      //      whether the tag came before or after the name.
      const stripped = raw.replace(/\[[^\]]*\]/g, " ").trim();
      const tokens = stripped.split(/\s+/).filter(Boolean);
      const name = tokens.find((t) => /^[a-zA-Z0-9]/.test(t)) ?? "";
      if (!name) {
        return JSON.stringify({
          error: "run_skill requires a 'name' argument",
          hint: `'${raw}' is just a marker/tag, not a skill name`,
        });
      }
      const skill = store.read(name);
      if (!skill) {
        const available = store
          .list()
          .map((s) => s.name)
          .join(", ");
        return JSON.stringify({
          error: `unknown skill: ${JSON.stringify(name)}`,
          available: available || "(none — user has not defined any skills)",
        });
      }
      const rawArgs = typeof args.arguments === "string" ? args.arguments.trim() : "";

      if (skill.runAs === "subagent") {
        if (!subagentRunner) {
          return JSON.stringify({
            error: `run_skill: skill ${JSON.stringify(name)} is marked runAs=subagent but no subagent runner is configured for this session. Skill authors who need isolation should run inside reasonix code (or a library setup that passes subagentRunner to registerSkillTools).`,
          });
        }
        if (!rawArgs) {
          return JSON.stringify({
            error: `run_skill: skill ${JSON.stringify(name)} is a subagent and requires 'arguments' — the subagent has no other context, so describe the concrete task in the arguments field.`,
          });
        }
        return subagentRunner(skill, rawArgs, ctx?.signal);
      }

      const header = [
        `# Skill: ${skill.name}`,
        skill.description ? `> ${skill.description}` : "",
        `(scope: ${skill.scope} · ${skill.path})`,
      ]
        .filter(Boolean)
        .join("\n");
      const argsBlock = rawArgs ? `\n\nArguments: ${rawArgs}` : "";
      const inner = `${header}\n\n${skill.body}${argsBlock}`;
      // Sentinel-wrapped so ContextManager.fold preserves the body verbatim instead of paraphrasing it.
      return `<skill-pin name=${JSON.stringify(skill.name)}>\n${inner}\n</skill-pin>`;
    },
  });

  const installScopeDesc = hasProjectScope
    ? "'project' (default) writes to <repo>/.reasonix/skills/, scoped to this workspace only; 'global' writes to ~/.reasonix/skills/, available in every project."
    : "'global' (only option here — no project workspace) writes to ~/.reasonix/skills/.";

  registry.register({
    name: "install_skill",
    description:
      "Author and save a new skill — a reusable playbook future turns can invoke via `run_skill`. Use when the same multi-step instruction would benefit from being callable by name instead of re-pasted. The skill is written to disk and runnable immediately (call `run_skill` with the same name in this very turn); it appears in the pinned Skills index only on the next `/new` or launch. WARNING: skill bodies become prompts for future agent turns — treat what you write as instructions you are giving your future self.",
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description:
            "Skill identifier — letters/digits/_/-/., 1-64 chars, starts alnum. Becomes the filename and what callers pass to `run_skill`.",
        },
        description: {
          type: "string",
          description:
            "One-line summary shown in the pinned Skills index. Keep under ~120 chars; this is what future agents read to decide whether to invoke the skill.",
        },
        body: {
          type: "string",
          description:
            "Full skill playbook in markdown — the instructions a future turn (or subagent) follows when this skill runs. For inline skills, write 'how to do X'. For subagent skills, write the subagent's persona + operating rules; remember the subagent has NO other context besides the `arguments` passed at runtime.",
        },
        scope: {
          type: "string",
          enum: ["project", "global"],
          description: installScopeDesc,
        },
        runAs: {
          type: "string",
          enum: ["inline", "subagent"],
          description:
            "'inline' (default) — body becomes a tool-result the parent agent reads and acts on (cheap, shares parent context). 'subagent' — spawns an isolated child loop; only the final answer returns to the parent (use when the work would flood context, e.g. exploration / research).",
        },
        model: {
          type: "string",
          description:
            "Optional model override for subagent skills (e.g. 'deepseek-chat'). Ignored for runAs=inline. Only `deepseek-*` ids are honored.",
        },
        allowedTools: {
          type: "array",
          items: { type: "string" },
          description:
            "Optional tool-name allowlist for subagent skills (e.g. ['read_file','search_content']). When set, the spawned subagent's registry is scoped to these literal names. Ignored for runAs=inline.",
        },
      },
      required: ["name", "description", "body"],
    },
    fn: async (args: {
      name?: unknown;
      description?: unknown;
      body?: unknown;
      scope?: unknown;
      runAs?: unknown;
      model?: unknown;
      allowedTools?: unknown;
    }) => {
      const name = typeof args.name === "string" ? args.name.trim() : "";
      const description =
        typeof args.description === "string"
          ? args.description.replace(/[\r\n]+/g, " ").trim()
          : "";
      const body = typeof args.body === "string" ? args.body : "";
      if (!name) return JSON.stringify({ error: "install_skill requires a non-empty 'name'" });
      if (!description) {
        return JSON.stringify({
          error:
            "install_skill requires a non-empty 'description' — it is what appears in the Skills index and how future agents decide whether to invoke the skill",
        });
      }
      if (!body.trim()) {
        return JSON.stringify({
          error:
            "install_skill requires a non-empty 'body' — the playbook the skill executes when invoked",
        });
      }

      const scopeRaw = typeof args.scope === "string" ? args.scope.trim() : "";
      let scope: "project" | "global";
      if (scopeRaw === "global") scope = "global";
      else if (scopeRaw === "project") scope = "project";
      else scope = hasProjectScope ? "project" : "global";
      if (scope === "project" && !hasProjectScope) {
        return JSON.stringify({
          error:
            "install_skill: scope='project' requires a workspace — run from `reasonix code`, or use scope='global'",
        });
      }

      const runAsRaw = typeof args.runAs === "string" ? args.runAs.trim() : "";
      const runAs: "inline" | "subagent" = runAsRaw === "subagent" ? "subagent" : "inline";

      const fmLines = ["---", `name: ${name}`, `description: ${description}`];
      if (runAs === "subagent") {
        fmLines.push("runAs: subagent");
        const model = typeof args.model === "string" ? args.model.trim() : "";
        if (model) fmLines.push(`model: ${model}`);
        if (Array.isArray(args.allowedTools)) {
          const tools = args.allowedTools
            .filter((t): t is string => typeof t === "string")
            .map((t) => t.trim())
            .filter(Boolean);
          if (tools.length > 0) fmLines.push(`allowed-tools: ${tools.join(", ")}`);
        }
      }
      fmLines.push("---", "");
      const content = `${fmLines.join("\n")}${body.replace(/\s+$/, "")}\n`;

      const result = store.createWithContent(name, scope, content);
      if ("error" in result) {
        return JSON.stringify({ error: result.error });
      }

      try {
        onSkillInstalled?.({ name, path: result.path, scope });
      } catch {
        // host hook failure must not undo a successful write
      }

      return JSON.stringify({
        ok: true,
        name,
        scope,
        path: result.path,
        runAs,
        note: "Skill is callable right now via run_skill({ name }). It will appear in the pinned Skills index after the next /new or launch.",
      });
    },
  });

  return registry;
}
