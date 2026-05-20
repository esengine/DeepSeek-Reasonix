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

interface BuiltinSubagentToolSpec {
  toolName: string;
  skillName: string;
  description: string;
  taskDescription: string;
}

function registerBuiltinSubagentTool(
  registry: ToolRegistry,
  store: SkillStore,
  subagentRunner: SubagentRunner | undefined,
  spec: BuiltinSubagentToolSpec,
): void {
  // Eager presence check — keeps disableBuiltins test mode clean (no
  // phantom tool spec when its skill body is absent).
  if (!store.read(spec.skillName)) return;
  registry.register({
    name: spec.toolName,
    description: spec.description,
    readOnly: true,
    parallelSafe: true,
    parameters: {
      type: "object",
      properties: {
        task: { type: "string", description: spec.taskDescription },
      },
      required: ["task"],
    },
    fn: async (args: { task?: unknown }, ctx) => {
      if (!subagentRunner) {
        return JSON.stringify({
          error: `${spec.toolName}: no subagent runner is configured for this session — run inside \`reasonix code\`, or pass \`subagentRunner\` to \`registerSkillTools\`.`,
        });
      }
      const task = typeof args.task === "string" ? args.task.trim() : "";
      if (!task) {
        return JSON.stringify({
          error: `${spec.toolName} requires a non-empty 'task' argument — describe the concrete question.`,
        });
      }
      const skill = store.read(spec.skillName);
      if (!skill) {
        return JSON.stringify({
          error: `${spec.toolName}: built-in skill ${JSON.stringify(spec.skillName)} is no longer registered`,
        });
      }
      // A user-supplied skill with the same name but `runAs: inline`
      // would silently lose isolation if we dispatched it here — bounce
      // back to run_skill where inline is well-defined.
      if (skill.runAs !== "subagent") {
        return JSON.stringify({
          error: `${spec.toolName}: skill ${JSON.stringify(spec.skillName)} is overridden as inline; invoke it via run_skill instead.`,
        });
      }
      return subagentRunner(skill, task, ctx?.signal);
    },
  });
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
      "Invoke a user-defined playbook from the Skills index pinned in the system prompt. **For the built-in subagent skills (explore / research / review / security_review), prefer the dedicated top-level tools by the same name — they're cheaper to pick and produce the same result.** Pass `name` as the BARE skill identifier (e.g. 'my-custom-skill'), NOT the `[🧬 subagent]` tag that appears after it in the index. Entries tagged `[🧬 subagent]` spawn an isolated subagent — only the final distilled answer comes back. Plain skills are inlined: the body becomes a tool result you read and follow. For subagent skills, supply 'arguments' describing the concrete task — they'll be the only context the subagent has.",
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

  // Top-level wrappers for built-in subagent skills. Same underlying
  // subagentRunner path as `run_skill(name="explore", ...)`, but the
  // tool name matches the verb in the question — models pick it
  // because affordance design > prompt rules.
  registerBuiltinSubagentTool(registry, store, subagentRunner, {
    toolName: "explore",
    skillName: "explore",
    description:
      "Run a focused read-only codebase investigation in an isolated subagent. **Use for broad survey questions across multiple files** — 'find all places that X', 'how does Y work across the project', 'audit Z'. Returns one distilled answer with file:line citations. Chained `read_file` is the wrong tool for these — it bloats your context with raw file contents; `explore`'s reads + reasoning never enter your log.",
    taskDescription:
      "Concrete investigation question. The subagent has none of your context — write a self-contained prompt naming the symbol / pattern / behavior you want surveyed.",
  });
  registerBuiltinSubagentTool(registry, store, subagentRunner, {
    toolName: "research",
    skillName: "research",
    description:
      "Combine web search + code reading in an isolated subagent. **Use when the answer needs both external reference and local verification** — 'is X supported by lib Y in version Z', 'compare our impl against the spec', 'what's the canonical way to do Q'. Returns one synthesis citing code (file:line) and web (URL). Reads + searches stay in the subagent.",
    taskDescription:
      "Concrete research question. The subagent has none of your context — name the external thing to look up and the local code to compare against.",
  });
  registerBuiltinSubagentTool(registry, store, subagentRunner, {
    toolName: "review",
    skillName: "review",
    description:
      "Review the pending changes (current branch diff) in an isolated subagent — flags correctness / security / missing-tests / hidden behavior per file:line. Read-only; you decide what to act on. Use before suggesting a PR-shaped change, or when you've finished a multi-step edit and want a second pass.",
    taskDescription:
      "What to focus the review on (e.g. 'focus on the auth changes' or 'general'). The subagent reads the diff itself.",
  });
  registerBuiltinSubagentTool(registry, store, subagentRunner, {
    toolName: "security_review",
    skillName: "security-review",
    description:
      "Security-focused review of current branch diff in an isolated subagent — injection / authz / secrets / deserialization / path-traversal / crypto issues, severity-tagged. Use when shipping changes that touch auth, input parsing, file IO, or external requests. Read-only.",
    taskDescription:
      "Optional scope hint (e.g. 'focus on token handling in src/auth/') or 'full' for everything in the diff.",
  });

  const installScopeDesc = hasProjectScope
    ? "'project' (default) writes to <repo>/.reasonix/skills/, scoped to this workspace only; 'global' writes to ~/.reasonix/skills/, available in every project."
    : "'global' (only option here — no project workspace) writes to ~/.reasonix/skills/.";

  registry.register({
    name: "install_skill",
    description:
      "Author and save a new skill — a reusable playbook future turns invoke via `run_skill`. Runnable immediately (same turn); appears in the pinned Skills index on next `/new` or launch. Skill bodies become prompts for future turns, so write what you'd want your future self to follow.",
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description:
            "Identifier — letters/digits/_/-/., 1-64 chars, starts alnum. Becomes the filename.",
        },
        description: {
          type: "string",
          description:
            "≤120 char one-liner shown in the pinned Skills index — future agents read this to decide whether to invoke.",
        },
        body: {
          type: "string",
          description:
            "Markdown playbook. For subagent skills, write the subagent's persona/rules — it gets no context besides `arguments` at runtime.",
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
            "inline (default) appends body to parent log. subagent spawns isolated child loop; only final answer returns (use for context-heavy work).",
        },
        model: {
          type: "string",
          description:
            "Optional `deepseek-*` model override for runAs=subagent. Ignored otherwise.",
        },
        allowedTools: {
          type: "array",
          items: { type: "string" },
          description:
            "Optional tool allowlist for runAs=subagent (e.g. ['read_file','search_content']).",
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
