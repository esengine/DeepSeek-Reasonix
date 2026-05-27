// First import — reject unsupported Node versions before heavier startup
// paths can turn an engine mismatch into an opaque crash.
import "./node-version-guard.js";

// Then re-exec with a bigger V8 heap when Node's stock 2 GiB cap is in force
// (issue #1011). Side-effect on module load, before any heavy import below runs.
import "./heap-limit-launch.js";

// Wrap stdout/stderr before any third-party lib gets a chance to emit BEL on
// Windows cmd, which would beep the system bell every render (#1786).
import "./strip-bel.js";

import { Command } from "commander";
import {
  ensureDashboardToken,
  isReasoningEffort,
  loadDashboardEnabled,
  loadProxyConfig,
  readConfig,
  saveReasoningEffort,
} from "../config.js";
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

function persistEffortFlag(flag: unknown): void {
  if (typeof flag !== "string") return;
  const v = flag.toLowerCase();
  if (!isReasoningEffort(v)) return;
  try {
    saveReasoningEffort(v);
  } catch {
    /* best-effort */
  }
}

// HTTPS_PROXY / HTTP_PROXY only reach Node's fetch via undici's global
// dispatcher; install before any client (DeepSeek, web tools, dashboard)
// constructs a fetch closure (#646). Argv is peeked manually here — commander
// hasn't run yet — so position of `--no-proxy` doesn't matter and we can
// honor it before any fetch closure captures the dispatcher.
const cliNoProxy = process.argv.includes("--no-proxy");
const cfgProxy = loadProxyConfig();
installProxyIfConfigured(process.env, {
  disabled: cliNoProxy || cfgProxy.disabled === true,
  url: cfgProxy.url,
  extraNoProxy: cfgProxy.noProxy,
  bypassDeepSeekDirect: cfgProxy.bypassDeepSeekDirect,
});

markPhase("cli_module_loaded");

function defaultSystemPrompt(modelId: string): string {
  return `You are Reasonix, a helpful DeepSeek-powered assistant. Be concise and accurate. Use tools when available.

# Tool Selection

When multiple tools serve the same purpose (e.g. web search), prefer MCP-provided tools over the built-in defaults — MCP tools like Exa typically offer higher quality (semantic search, date / domain filtering). If an MCP tool fails, fall back to the built-in.

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
