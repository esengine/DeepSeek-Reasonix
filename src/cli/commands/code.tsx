/**
 * `reasonix code [dir]` — opinionated wrapper around `reasonix chat` for
 * code-editing workflows.
 *
 * What it does differently from plain chat:
 *   - Registers native filesystem tools rooted at the given directory
 *     (CWD by default). No subprocess, no `npx install` step, R1-
 *     friendly schemas. Replaced the old `@modelcontextprotocol/server-filesystem`
 *     subprocess in 0.4.9 because its `edit_file` argv shape was the
 *     biggest driver of R1 DSML hallucinations.
 *   - Uses a coding-focused system prompt (src/code/prompt.ts) that
 *     teaches the model to propose edits as SEARCH/REPLACE blocks.
 *   - Defaults to the `smart` preset (reasoner + harvest) because
 *     coding tasks pay back R1 thinking.
 *   - Scopes its session to the directory so projects don't share
 *     conversation history.
 *   - Hooks `codeMode` into the TUI so assistant replies get parsed
 *     for SEARCH/REPLACE blocks and applied on disk after each turn.
 */

import { readFileSync } from "node:fs";
import { basename, resolve } from "node:path";
import { buildCodeToolset } from "../../code/setup.js";
import { loadApiKey, loadPreset, readConfig } from "../../config.js";
import { loadDotenv } from "../../env.js";
import { t } from "../../i18n/index.js";
import { detectForeignAgentPlatform } from "../../memory/project.js";
import { sanitizeName } from "../../memory/session.js";
import { markPhase } from "../startup-profile.js";
import { resolvePreset } from "../ui/presets.js";
import { chatCommand } from "./chat.js";

export interface CodeOptions {
  /** Directory to root the filesystem tools at. Defaults to process.cwd(). */
  dir?: string;
  /** Override the default `smart` model. */
  model?: string;
  /** Disable session persistence. */
  noSession?: boolean;
  /** Transcript file for replay/diff. */
  transcript?: string;
  /** Skip the session picker — always resume prior messages. */
  forceResume?: boolean;
  /** Skip the session picker — always wipe prior messages and start fresh. */
  forceNew?: boolean;
  /**
   * Soft USD spend cap. Off by default. Same semantics as `chat`:
   * warns at 80%, refuses next turn at 100%. Mid-session adjustable
   * via `/budget <usd>` slash command.
   */
  budgetUsd?: number;
  /** Per-turn repair-signal count required to escalate flash→pro. Undefined → loop default (3). */
  failureThreshold?: number;
  /** Suppress the auto-launched embedded web dashboard. */
  noDashboard?: boolean;
  /** When true and the dashboard is enabled, open its URL in the system default browser as soon as the server is ready. */
  openDashboard?: boolean;
  /** Pin the dashboard to a fixed port. `undefined` keeps ephemeral assignment. */
  dashboardPort?: number;
  /** Inline string appended to the code system prompt after the generated base prompt. */
  systemAppend?: string;
  /** Path to a UTF-8 text file whose contents are appended to the code system prompt. */
  systemAppendFile?: string;
  /** Default true. Pass false (CLI: `--no-alt-screen`) to keep chat output in shell scrollback. */
  altScreen?: boolean;
  /** Default true. Pass false (CLI: `--no-mouse`) to keep terminal-native drag-select unmodified. */
  mouse?: boolean;
}

export async function codeCommand(opts: CodeOptions = {}): Promise<void> {
  markPhase("code_command_enter");
  const resolvedModel = opts.model ?? resolvePreset(loadPreset()).model;
  // Bridge .env + ~/.reasonix/config.json into process.env so buildCodeToolset's
  // eager DeepSeekClient constructions (subagent client; semantic embedder) can
  // pick up a key the user already configured via `reasonix setup`. chatCommand
  // does the same dance — code.tsx wraps chatCommand but must also seed env
  // before buildCodeToolset runs, which is BEFORE chatCommand.
  loadDotenv();
  const cfgKey = loadApiKey();
  if (cfgKey && !process.env.DEEPSEEK_API_KEY) {
    process.env.DEEPSEEK_API_KEY = cfgKey;
  }
  const { codeSystemPrompt } = await import("../../code/prompt.js");
  const rootDir = resolve(opts.dir ?? process.cwd());
  // Per-directory session so switching projects doesn't mix histories.
  // `code-<sanitized-basename>` fits the session name rules without
  // truncating most project names.
  const session = opts.noSession ? undefined : `code-${sanitizeName(basename(rootDir))}`;

  markPhase("semantic_bootstrap_start");
  const { tools, jobs, registerRooted, reBootstrapSemantic, semantic } = await buildCodeToolset({
    rootDir,
  });
  markPhase(
    semantic.enabled ? "semantic_bootstrap_done_enabled" : "semantic_bootstrap_done_skipped",
  );

  process.stderr.write(
    `${t("startup.codeRooted", {
      rootDir,
      session: session ?? t("startup.ephemeral"),
      tools: tools.size,
      semantic: semantic.enabled ? t("startup.semanticOn") : "",
    })}\n`,
  );

  const foreign = detectForeignAgentPlatform(rootDir);
  if (foreign) {
    process.stderr.write(t("code.workspaceConflict", { platforms: foreign.join(", ") }));
  }

  // Belt-and-suspenders cleanup: even though spawn(detached:false)
  // should tie child processes to the parent's lifetime, Windows cmd.exe
  // wrappers occasionally leak. We DON'T install SIGINT/SIGTERM
  // handlers here — that overrode Node's default "exit on Ctrl+C" with
  // a silent no-op, which made Ctrl+C feel broken in the TUI. App.tsx
  // owns the SIGINT path now (it shows the quit-armed banner and calls
  // exit() on confirmation); this 'exit' hook just guarantees the job
  // registry is drained on the way out, regardless of which exit path
  // fired.
  process.once("exit", () => {
    void jobs.shutdown();
  });

  let systemAppendFileContents: string | undefined;
  if (opts.systemAppend !== undefined && opts.systemAppend.trim().length === 0) {
    process.stderr.write(t("code.systemAppendEmpty"));
  }
  if (opts.systemAppendFile) {
    const filePath = resolve(opts.systemAppendFile);
    try {
      systemAppendFileContents = readFileSync(filePath, "utf8");
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      const errorDetails = e.code ? `[${e.code}] ${e.message}` : e.message;
      process.stderr.write(t("code.systemAppendFileReadError", { filePath, errorDetails }));
      process.exit(1);
    }
  }

  const codeRebuildSystem = () =>
    codeSystemPrompt(rootDir, {
      hasSemanticSearch: semantic.enabled,
      systemAppend: opts.systemAppend,
      systemAppendFile: systemAppendFileContents,
      modelId: resolvedModel,
    });
  await chatCommand({
    model: resolvedModel,
    budgetUsd: opts.budgetUsd,
    failureThreshold: opts.failureThreshold,
    system: codeRebuildSystem(),
    rebuildSystem: codeRebuildSystem,
    transcript: opts.transcript,
    session,
    seedTools: tools,
    codeMode: {
      rootDir,
      jobs,
      reregisterTools: registerRooted,
      reBootstrapSemantic,
    },
    mcp: readConfig().mcp,
    forceResume: opts.forceResume,
    forceNew: opts.forceNew,
    noDashboard: opts.noDashboard,
    openDashboard: opts.openDashboard,
    dashboardPort: opts.dashboardPort,
    altScreen: opts.altScreen,
    mouse: opts.mouse,
  });
}
