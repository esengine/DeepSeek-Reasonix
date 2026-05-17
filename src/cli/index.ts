// First import — re-execs the process with a bigger V8 heap when Node's
// stock 2 GiB cap is in force (issue #1011). Side-effect on module load,
// before any heavy import below runs.
import "./heap-limit-launch.js";

import { Command } from "commander";
import { readConfig } from "../config.js";
import { t } from "../i18n/index.js";
import { VERSION } from "../index.js";
import { listSessions } from "../memory/session.js";
import { applyMemoryStack } from "../memory/user.js";
import { installProxyIfConfigured } from "../net/proxy.js";
import { escalationContract } from "../prompt-fragments.js";
import { startCpuProfile, stopAndSaveCpuProfile } from "./cpu-prof.js";
import { resolveBareCommandMode, resolveContinueFlag, resolveDefaults } from "./resolve.js";
import { markPhase } from "./startup-profile.js";

async function maybeStartCpuProfile(flag: unknown): Promise<boolean> {
  if (flag === undefined || flag === false) return false;
  await startCpuProfile(typeof flag === "string" ? flag : undefined);
  return true;
}

// HTTPS_PROXY / HTTP_PROXY only reach Node's fetch via undici's global
// dispatcher; install before any client (DeepSeek, web tools, dashboard)
// constructs a fetch closure. Issue #646.
installProxyIfConfigured();

markPhase("cli_module_loaded");

function defaultSystemPrompt(modelId: string): string {
  return `You are Reasonix, a helpful DeepSeek-powered assistant. Be concise and accurate. Use tools when available.

# Cite or shut up — non-negotiable

Every factual claim about a codebase must be backed by evidence. Reasonix VALIDATES your citations — broken paths render in **red strikethrough with ❌** in front of the user.

**Positive claims** — append a markdown link:
- ✅ \`The MCP client supports listResources [listResources](src/mcp/client.ts:142).\`
- ❌ \`The MCP client supports listResources.\` ← unverifiable, do not write.

**Negative claims** ("X is missing", "Y isn't implemented", "lacks Z") are the #1 hallucination shape. STOP before writing them. If you have a search tool, call it first; if the search returns nothing, cite the search itself as evidence (\`No matches for "foo" in src/\`). If you have no tool, qualify hard: "I haven't verified — this is a guess."

Asserting absence without checking is how evaluative answers go wrong. Treat the urge to write "missing" as a red flag in your own reasoning.

# Don't invent what changes — search instead

Your training data has a cutoff. When an answer's correctness depends on something that changes over time (the user is asking what's happening, not what's true) and a search tool is available, search first. Inventing currently-correct values from training memory is the most common way these answers go wrong, and the user usually can't tell until much later.

The signal isn't a topic list — it's: "if I'm wrong about this, is it because reality moved on?". If yes, ground the answer in fresh evidence; if no (definitions, mechanisms, well-established APIs), answer from memory.

${escalationContract(modelId)}`;
}

/** Lenient: malformed → undefined (no cap) so a bad flag doesn't abort launch. */
function parseBudgetFlag(raw: number | undefined): number | undefined {
  if (raw === undefined) return undefined;
  if (!Number.isFinite(raw) || raw <= 0) {
    process.stderr.write(
      `▲ ignoring --budget=${raw} (must be a positive number) — running with no cap\n`,
    );
    return undefined;
  }
  return raw;
}

/** Lenient: malformed → undefined so a bad flag falls back to config / default. */
function parseEscalateAfterFlag(raw: number | undefined): number | undefined {
  if (raw === undefined) return undefined;
  if (!Number.isInteger(raw) || raw < 1 || raw > 20) {
    process.stderr.write(
      `▲ ignoring --escalate-after=${raw} (must be an integer in [1,20]) — using default\n`,
    );
    return undefined;
  }
  return raw;
}

function resolveFailureThreshold(
  flagValue: number | undefined,
  noConfig: boolean,
): number | undefined {
  if (flagValue !== undefined) return flagValue;
  if (noConfig) return undefined;
  const fromCfg = readConfig().escalation?.failureThreshold;
  if (typeof fromCfg !== "number") return undefined;
  if (!Number.isInteger(fromCfg) || fromCfg < 1 || fromCfg > 20) {
    process.stderr.write(
      `▲ ignoring escalation.failureThreshold=${fromCfg} from config (must be an integer in [1,20]) — using default\n`,
    );
    return undefined;
  }
  return fromCfg;
}

/** Lenient port parser — bad value warns + falls back to ephemeral, same shape as parseBudgetFlag. */
function parseDashboardPortFlag(raw: string | undefined): number | undefined {
  if (raw === undefined) return undefined;
  const n = Number.parseInt(raw, 10);
  if (!Number.isInteger(n) || n < 1 || n > 65535) {
    process.stderr.write(`${t("ui.dashboardPortInvalid", { value: raw })}\n`);
    return undefined;
  }
  return n;
}

function resolveDashboardPort(
  flagValue: number | undefined,
  noConfig: boolean,
): number | undefined {
  if (flagValue !== undefined) return flagValue;
  if (noConfig) return undefined;
  const fromCfg = readConfig().dashboard?.port;
  return typeof fromCfg === "number" &&
    Number.isInteger(fromCfg) &&
    fromCfg >= 1 &&
    fromCfg <= 65535
    ? fromCfg
    : undefined;
}

/** Resolution order: flag → REASONIX_DASHBOARD_HOST env → config.dashboard.host → undefined (server defaults to 127.0.0.1). */
function resolveDashboardHost(
  flagValue: string | undefined,
  noConfig: boolean,
): string | undefined {
  const fromFlag = flagValue?.trim();
  if (fromFlag) return fromFlag;
  const fromEnv = process.env.REASONIX_DASHBOARD_HOST?.trim();
  if (fromEnv) return fromEnv;
  if (noConfig) return undefined;
  const fromCfg = readConfig().dashboard?.host;
  return typeof fromCfg === "string" && fromCfg.trim() ? fromCfg.trim() : undefined;
}

/** Resolution order: REASONIX_DASHBOARD_TOKEN env → config.dashboard.token → undefined (server mints a fresh per-boot token). Min 16 chars; shorter values are dropped with a warning to avoid trivially-guessable tokens. */
function resolveDashboardToken(noConfig: boolean): string | undefined {
  const fromEnv = process.env.REASONIX_DASHBOARD_TOKEN?.trim();
  const fromCfg = noConfig ? undefined : readConfig().dashboard?.token?.trim();
  const candidate = fromEnv || fromCfg;
  if (!candidate) return undefined;
  if (candidate.length < 16) {
    process.stderr.write(
      `▲ ignoring dashboard token (${candidate.length} chars; min 16) — using ephemeral per-boot token instead\n`,
    );
    return undefined;
  }
  return candidate;
}

const program = new Command();
program
  .name("reasonix")
  .description(t("cli.description"))
  .version(VERSION)
  .option("-c, --continue", t("cli.continue"));

// `reasonix` with no subcommand → setup wizard on first run, otherwise `code`
// in the current directory. Filesystem-less chat stays reachable via
// `reasonix chat`.
program
  .option("--node", "use the legacy Node/Ink TUI (default is the Rust renderer)")
  .action(async (opts: { continue?: boolean; node?: boolean }) => {
    if (opts.node) process.env.REASONIX_RENDERER = "node";
    const cfg = readConfig();
    const mode = resolveBareCommandMode(cfg);
    if (mode === "setup") {
      const { setupCommand } = await import("./commands/setup.js");
      await setupCommand({ forceKeyStep: true });
      return;
    }
    const { codeCommand } = await import("./commands/code.js");
    await codeCommand({ dir: process.cwd(), forceResume: !!opts.continue });
  });

program
  .command("setup")
  .description(t("cli.setup"))
  .action(async () => {
    const { setupCommand } = await import("./commands/setup.js");
    await setupCommand({ forceKeyStep: true });
  });

program
  .command("code [dir]")
  .description(t("cli.code"))
  .option("-m, --model <id>", t("ui.modelOverride"))
  .option("--no-session", t("ui.noSession"))
  .option("-r, --resume", t("ui.resumeHint"))
  .option("-n, --new", t("ui.newHint"))
  .option("--transcript <path>", t("ui.transcriptHint"))
  .option("--budget <usd>", t("ui.budgetHint"), (v) => Number.parseFloat(v))
  .option(
    "--escalate-after <n>",
    "repair-signal count before flash→pro escalation (default 3)",
    (v) => Number.parseInt(v, 10),
  )
  .option("--no-dashboard", t("ui.noDashboard"))
  .option("--open-dashboard", t("ui.openDashboardHint"))
  .option("--dashboard-port <port>", t("ui.dashboardPortHint"))
  .option(
    "--dashboard-host <host>",
    "bind address for the dashboard (default 127.0.0.1; use 0.0.0.0 for LAN access — the URL token is then the only auth)",
  )
  .option("--no-alt-screen", "keep chat output in shell scrollback (legacy mode, ghost-prone)")
  .option("--no-mouse", "disable SGR mouse tracking (keeps drag-select 100% native)")
  .option("--system-append <prompt>", t("ui.systemAppendHint"))
  .option("--system-append-file <path>", t("ui.systemAppendFileHint"))
  .option(
    "--profile [path]",
    "record a V8 CPU profile; saved on exit. Send the .cpuprofile back if you're reporting a perf bug.",
  )
  .option("--node", "use the legacy Node/Ink TUI (default is the Rust renderer)")
  .action(async (dir: string | undefined, opts) => {
    if (opts.node) process.env.REASONIX_RENDERER = "node";
    const profiling = await maybeStartCpuProfile(opts.profile);
    try {
      const { codeCommand } = await import("./commands/code.js");
      await codeCommand({
        dir,
        model: opts.model,
        noSession: opts.session === false,
        transcript: opts.transcript,
        forceResume: !!opts.resume,
        forceNew: !!opts.new,
        budgetUsd: parseBudgetFlag(opts.budget),
        failureThreshold: resolveFailureThreshold(
          parseEscalateAfterFlag(opts.escalateAfter),
          false,
        ),
        noDashboard: opts.dashboard === false,
        openDashboard: opts.openDashboard === true,
        dashboardPort: resolveDashboardPort(parseDashboardPortFlag(opts.dashboardPort), false),
        dashboardHost: resolveDashboardHost(opts.dashboardHost, false),
        dashboardToken: resolveDashboardToken(false),
        systemAppend: opts.systemAppend,
        systemAppendFile: opts.systemAppendFile,
        altScreen: opts.altScreen !== false,
        mouse: opts.mouse !== false,
      });
    } finally {
      if (profiling) await stopAndSaveCpuProfile();
    }
  });

program
  .command("chat")
  .description(t("cli.chat"))
  .option("-m, --model <id>", t("ui.modelIdHint"))
  .option("-s, --system <prompt>", t("ui.systemPromptHint"))
  .option("--transcript <path>", t("ui.transcriptHint"))
  .option("--preset <name>", t("ui.presetHint"))
  .option("--budget <usd>", t("ui.budgetHint"), (v) => Number.parseFloat(v))
  .option(
    "--escalate-after <n>",
    "repair-signal count before flash→pro escalation (default 3)",
    (v) => Number.parseInt(v, 10),
  )
  .option("--session <name>", t("ui.sessionNameHint"))
  .option("--no-session", t("ui.ephemeralHint"))
  .option("-r, --resume", t("ui.resumeHint"))
  .option("-c, --continue", t("cli.continue"))
  .option("-n, --new", t("ui.newHint"))
  .option(
    "--mcp <spec>",
    t("ui.mcpSpecHint"),
    (value: string, previous: string[] = []) => [...previous, value],
    [] as string[],
  )
  .option("--mcp-prefix <str>", t("ui.mcpPrefixHint"))
  .option("--no-config", t("ui.noConfigHint"))
  .option("--no-dashboard", t("ui.noDashboard"))
  .option("--open-dashboard", t("ui.openDashboardHint"))
  .option("--dashboard-port <port>", t("ui.dashboardPortHint"))
  .option(
    "--dashboard-host <host>",
    "bind address for the dashboard (default 127.0.0.1; use 0.0.0.0 for LAN access — the URL token is then the only auth)",
  )
  .option("--no-alt-screen", "keep chat output in shell scrollback (legacy mode, ghost-prone)")
  .option("--no-mouse", "disable SGR mouse tracking (keeps drag-select 100% native)")
  .option(
    "--profile [path]",
    "record a V8 CPU profile; saved on exit. Send the .cpuprofile back if you're reporting a perf bug.",
  )
  .option("--node", "use the legacy Node/Ink TUI (default is the Rust renderer)")
  .action(async (opts) => {
    if (opts.node) process.env.REASONIX_RENDERER = "node";
    const profiling = await maybeStartCpuProfile(opts.profile);
    try {
      const defaults = resolveDefaults({
        model: opts.model,
        mcp: opts.mcp as string[],
        session: opts.session,
        preset: opts.preset,
        noConfig: opts.config === false,
      });
      // `-c` is "newest-touched session" + auto-resume; `-r` is "this
      // session's prior messages, even if you also passed --session".
      // When both are set we prefer the explicit `--session` + `-r`
      // (more specific input wins). `-c` only kicks in if `-r` wasn't.
      const continueOpts = opts.resume
        ? { session: defaults.session, forceResume: true }
        : resolveContinueFlag(
            opts.continue,
            defaults.session,
            () => listSessions()[0],
            (msg) => process.stderr.write(`${msg}\n`),
          );
      const { chatCommand } = await import("./commands/chat.js");
      const chatBase = opts.system ?? defaultSystemPrompt(defaults.model);
      const chatCwd = process.cwd();
      const chatRebuildSystem = () => applyMemoryStack(chatBase, chatCwd);
      await chatCommand({
        model: defaults.model,
        system: chatRebuildSystem(),
        rebuildSystem: chatRebuildSystem,
        transcript: opts.transcript,
        budgetUsd: parseBudgetFlag(opts.budget),
        failureThreshold: resolveFailureThreshold(
          parseEscalateAfterFlag(opts.escalateAfter),
          opts.config === false,
        ),
        session: continueOpts.session,
        mcp: defaults.mcp,
        mcpPrefix: opts.mcpPrefix,
        forceResume: continueOpts.forceResume,
        forceNew: !!opts.new,
        noDashboard: opts.dashboard === false,
        openDashboard: opts.openDashboard === true,
        dashboardPort: resolveDashboardPort(
          parseDashboardPortFlag(opts.dashboardPort),
          opts.config === false,
        ),
        dashboardHost: resolveDashboardHost(opts.dashboardHost, opts.config === false),
        dashboardToken: resolveDashboardToken(opts.config === false),
        altScreen: opts.altScreen !== false,
        mouse: opts.mouse !== false,
      });
    } finally {
      if (profiling) await stopAndSaveCpuProfile();
    }
  });

program
  .command("run <task>")
  .description(t("cli.run"))
  .option("-m, --model <id>", t("ui.modelIdHint"))
  .option("-s, --system <prompt>", t("ui.systemPromptHint"))
  .option("--preset <name>", t("ui.presetHintShort"))
  .option("--budget <usd>", t("ui.budgetHintShort"), (v) => Number.parseFloat(v))
  .option(
    "--escalate-after <n>",
    "repair-signal count before flash→pro escalation (default 3)",
    (v) => Number.parseInt(v, 10),
  )
  .option("--transcript <path>", t("ui.transcriptHintShort"))
  .option(
    "--mcp <spec>",
    t("ui.mcpSpecHintShort"),
    (value: string, previous: string[] = []) => [...previous, value],
    [] as string[],
  )
  .option("--mcp-prefix <str>", t("ui.mcpPrefixHintShort"))
  .option("--no-config", t("ui.noConfigHint"))
  .action(async (task: string, opts) => {
    const defaults = resolveDefaults({
      model: opts.model,
      mcp: opts.mcp as string[],
      preset: opts.preset,
      noConfig: opts.config === false,
    });
    const { runCommand } = await import("./commands/run.js");
    await runCommand({
      task,
      model: defaults.model,
      system: applyMemoryStack(opts.system ?? defaultSystemPrompt(defaults.model), process.cwd()),
      budgetUsd: parseBudgetFlag(opts.budget),
      failureThreshold: resolveFailureThreshold(
        parseEscalateAfterFlag(opts.escalateAfter),
        opts.config === false,
      ),
      transcript: opts.transcript,
      mcp: defaults.mcp,
      mcpPrefix: opts.mcpPrefix,
    });
  });

program
  .command("acp")
  .description("run reasonix as an Agent Client Protocol (ACP) agent on stdio NDJSON JSON-RPC")
  .option("-m, --model <id>", t("ui.modelIdHint"))
  .option("--dir <path>", "root directory for filesystem tools (default: cwd)")
  .option("--preset <name>", t("ui.presetHintShort"))
  .option("--budget <usd>", t("ui.budgetHintShort"), (v) => Number.parseFloat(v))
  .option("--transcript <path>", t("ui.transcriptHint"))
  .option("--yolo", t("ui.yoloHint"))
  .option(
    "--mcp <spec>",
    t("ui.mcpSpecHintShort"),
    (value: string, previous: string[] = []) => [...previous, value],
    [] as string[],
  )
  .option("--mcp-prefix <str>", t("ui.mcpPrefixHintShort"))
  .action(async (opts) => {
    const defaults = resolveDefaults({
      model: opts.model,
      mcp: opts.mcp as string[],
      preset: opts.preset,
      noConfig: false,
    });
    const { acpCommand } = await import("./commands/acp.js");
    await acpCommand({
      model: defaults.model,
      budgetUsd: parseBudgetFlag(opts.budget),
      dir: opts.dir,
      transcript: opts.transcript,
      yolo: !!opts.yolo,
      mcpSpecs: defaults.mcp,
      mcpPrefix: opts.mcpPrefix,
    });
  });

program
  .command("desktop")
  .description("headless JSON-RPC chat for the desktop client (internal)")
  .option("-m, --model <id>", t("ui.modelIdHint"))
  .option("--dir <path>", "root directory for filesystem tools (default: cwd)")
  .option("--preset <name>", t("ui.presetHintShort"))
  .option("--budget <usd>", t("ui.budgetHintShort"), (v) => Number.parseFloat(v))
  .action(async (opts) => {
    const defaults = resolveDefaults({
      model: opts.model,
      mcp: [],
      preset: opts.preset,
      noConfig: false,
    });
    const { desktopCommand } = await import("./commands/desktop.js");
    await desktopCommand({
      model: defaults.model,
      budgetUsd: parseBudgetFlag(opts.budget),
      dir: opts.dir,
    });
  });

program
  .command("stats [transcript]")
  .description(t("cli.stats"))
  .action(async (transcript: string | undefined) => {
    const { statsCommand } = await import("./commands/stats.js");
    statsCommand({ transcript });
  });

program
  .command("doctor")
  .description(t("cli.doctor"))
  .option("--json", t("ui.jsonHint"))
  .action(async (opts) => {
    const { doctorCommand } = await import("./commands/doctor.js");
    await doctorCommand({ json: !!opts.json });
  });

program
  .command("commit")
  .description(t("cli.commit"))
  .option("-m, --model <id>", t("ui.modelOverrideFlash"))
  .option("-y, --yes", t("ui.skipConfirmHint"))
  .action(async (opts) => {
    const { commitCommand } = await import("./commands/commit.js");
    await commitCommand({ model: opts.model, yes: !!opts.yes });
  });

program
  .command("sessions [name]")
  .description(t("cli.sessions"))
  .option("-v, --verbose", t("ui.verboseHint"))
  .action(async (name: string | undefined, opts) => {
    const { sessionsCommand } = await import("./commands/sessions.js");
    sessionsCommand({ name, verbose: !!opts.verbose });
  });

program
  .command("prune-sessions")
  .description(t("cli.pruneSessions"))
  .option("--days <n>", t("ui.pruneDaysHint"), (v) => Number.parseInt(v, 10))
  .option("--dry-run", t("ui.pruneDryRunHint"))
  .action(async (opts) => {
    const { pruneSessionsCommand } = await import("./commands/prune-sessions.js");
    pruneSessionsCommand({ days: opts.days, dryRun: !!opts.dryRun });
  });

program
  .command("events <name>")
  .description(t("cli.events"))
  .option("--type <type>", t("ui.eventTypeHint"))
  .option("--since <id>", t("ui.eventSinceHint"), (v) => Number.parseInt(v, 10))
  .option("--tail <n>", t("ui.eventTailHint"), (v) => Number.parseInt(v, 10))
  .option("--json", t("ui.jsonHint"))
  .option("--projection", t("ui.projectionHint"))
  .action(async (name: string, opts) => {
    const { eventsCommand } = await import("./commands/events.js");
    eventsCommand({
      name,
      type: opts.type,
      since: Number.isFinite(opts.since) ? opts.since : undefined,
      tail: Number.isFinite(opts.tail) ? opts.tail : undefined,
      json: !!opts.json,
      projection: !!opts.projection,
    });
  });

program
  .command("replay <transcript>")
  .description(t("cli.replay"))
  .option("--print", t("ui.printHint"))
  .option("--head <n>", t("ui.headHint"), (v) => Number.parseInt(v, 10))
  .option("--tail <n>", t("ui.tailHint"), (v) => Number.parseInt(v, 10))
  .action(async (transcript: string, opts) => {
    const { replayCommand } = await import("./commands/replay.js");
    await replayCommand({
      path: transcript,
      print: !!opts.print,
      head: Number.isFinite(opts.head) ? opts.head : undefined,
      tail: Number.isFinite(opts.tail) ? opts.tail : undefined,
    });
  });

program
  .command("diff <a> <b>")
  .description(t("cli.diff"))
  .option("--md <path>", t("ui.mdReportHint"))
  .option("--print", t("ui.printHintTable"))
  .option("--tui", t("ui.tuiHint"))
  .option("--label-a <label>", t("ui.labelAHint"))
  .option("--label-b <label>", t("ui.labelBHint"))
  .action(async (a: string, b: string, opts) => {
    const { diffCommand } = await import("./commands/diff.js");
    await diffCommand({
      a,
      b,
      mdPath: opts.md,
      labelA: opts.labelA,
      labelB: opts.labelB,
      print: !!opts.print,
      tui: !!opts.tui,
    });
  });

const mcp = program.command("mcp").description(t("cli.mcp"));

mcp
  .command("list")
  .description(t("ui.mcpListDescription"))
  .option("--json", t("ui.jsonHintCatalog"))
  .option("--local", t("ui.mcpLocalHint"))
  .option("--refresh", t("ui.mcpRefreshHint"))
  .option("--limit <n>", t("ui.mcpLimitHint"), (v) => Number.parseInt(v, 10))
  .option("--pages <n>", t("ui.mcpPagesHint"), (v) => Number.parseInt(v, 10))
  .option("--all", t("ui.mcpAllHint"))
  .action(async (opts) => {
    try {
      const { mcpListCommand } = await import("./commands/mcp.js");
      await mcpListCommand({
        json: !!opts.json,
        local: !!opts.local,
        refresh: !!opts.refresh,
        limit: typeof opts.limit === "number" && opts.limit > 0 ? opts.limit : undefined,
        pages: typeof opts.pages === "number" && opts.pages > 0 ? opts.pages : undefined,
        all: !!opts.all,
      });
    } catch (err) {
      process.stderr.write(`mcp list failed: ${(err as Error).message}\n`);
      process.exit(1);
    }
  });

mcp
  .command("search <query>")
  .description(t("ui.mcpSearchDescription"))
  .option("--json", t("ui.jsonHintCatalog"))
  .option("--refresh", t("ui.mcpRefreshHint"))
  .option("--limit <n>", t("ui.mcpLimitHint"), (v) => Number.parseInt(v, 10))
  .option("--max-pages <n>", t("ui.mcpMaxPagesHint"), (v) => Number.parseInt(v, 10))
  .action(async (query: string, opts) => {
    try {
      const { mcpSearchCommand } = await import("./commands/mcp.js");
      await mcpSearchCommand(query, {
        json: !!opts.json,
        refresh: !!opts.refresh,
        limit: typeof opts.limit === "number" && opts.limit > 0 ? opts.limit : undefined,
        maxPages:
          typeof opts.maxPages === "number" && opts.maxPages > 0 ? opts.maxPages : undefined,
      });
    } catch (err) {
      process.stderr.write(`mcp search failed: ${(err as Error).message}\n`);
      process.exit(1);
    }
  });

mcp
  .command("install <name>")
  .description(t("ui.mcpInstallDescription"))
  .option("--refresh", t("ui.mcpRefreshHint"))
  .option("--max-pages <n>", t("ui.mcpMaxPagesHint"), (v) => Number.parseInt(v, 10))
  .action(async (name: string, opts) => {
    try {
      const { mcpInstallCommand } = await import("./commands/mcp.js");
      await mcpInstallCommand(name, {
        refresh: !!opts.refresh,
        maxPages:
          typeof opts.maxPages === "number" && opts.maxPages > 0 ? opts.maxPages : undefined,
      });
    } catch (err) {
      process.stderr.write(`mcp install failed: ${(err as Error).message}\n`);
      process.exit(1);
    }
  });

mcp
  .command("browse")
  .description(t("ui.mcpBrowseDescription"))
  .action(async () => {
    try {
      const { mcpBrowseCommand } = await import("./commands/mcp-browse.js");
      await mcpBrowseCommand();
    } catch (err) {
      process.stderr.write(`mcp browse failed: ${(err as Error).message}\n`);
      process.exit(1);
    }
  });

mcp
  .command("inspect <spec>")
  .description(t("ui.mcpInspectDescription"))
  .option("--json", t("ui.jsonHintReport"))
  .action(async (spec: string, opts) => {
    const { formatMcpInspectFailure, mcpInspectCommand } = await import(
      "./commands/mcp-inspect.js"
    );
    try {
      await mcpInspectCommand({ spec, json: !!opts.json });
    } catch (err) {
      process.stderr.write(`mcp inspect failed: ${formatMcpInspectFailure(err)}\n`);
      process.exit(1);
    }
  });

program
  .command("version")
  .description(t("cli.version"))
  .action(async () => {
    const { versionCommand } = await import("./commands/version.js");
    versionCommand();
  });

program
  .command("update")
  .description(t("cli.update"))
  .option("--dry-run", t("ui.dryRunHint"))
  .action(async (opts: { dryRun?: boolean }) => {
    const { updateCommand } = await import("./commands/update.js");
    await updateCommand({ dryRun: !!opts.dryRun });
  });

program
  .command("index")
  .description(t("cli.index"))
  .option("--rebuild", t("ui.rebuildHint"))
  .option("--model <name>", t("ui.embedModelHint"))
  .option("--dir <path>", t("ui.projectDirHint"))
  .option("--ollama-url <url>", t("ui.ollamaUrlHint"))
  .option("-y, --yes", t("ui.skipPromptsHint"))
  .action(
    async (opts: {
      rebuild?: boolean;
      model?: string;
      dir?: string;
      ollamaUrl?: string;
      yes?: boolean;
    }) => {
      const { indexCommand } = await import("./commands/index.js");
      await indexCommand(opts);
    },
  );

program.parseAsync(process.argv).catch((err) => {
  console.error(err);
  process.exit(1);
});
