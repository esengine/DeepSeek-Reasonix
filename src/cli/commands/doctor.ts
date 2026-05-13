/** Plain-text (not Ink) — must work when everything else is broken. fail → exit 1; warn → exit 0. */

import { existsSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { DeepSeekClient, pickPrimaryBalance } from "../../client.js";
import {
  defaultConfigPath,
  loadBaseUrl,
  readConfig,
  resolveSemanticEmbeddingConfig,
} from "../../config.js";
import { loadDotenv } from "../../env.js";
import { loadHooks } from "../../hooks.js";
import { t } from "../../i18n/index.js";
import { indexExists } from "../../index/semantic/builder.js";
import { checkOllamaStatus } from "../../index/semantic/ollama-launcher.js";
import { listSessions } from "../../memory/session.js";
import { detectProxyUrl } from "../../net/proxy.js";
import { resolveDataPath } from "../../tokenizer.js";
import { VERSION } from "../../version.js";

export type DoctorLevel = "ok" | "warn" | "fail";

export interface DoctorCheck {
  id: string;
  label: string;
  level: DoctorLevel;
  detail: string;
}

export interface DoctorOptions {
  json?: boolean;
}

type Level = DoctorLevel;
type Check = DoctorCheck;

export async function runDoctorChecks(projectRoot: string): Promise<DoctorCheck[]> {
  return Promise.all([
    checkApiKey(),
    checkConfig(),
    checkProxy(),
    checkApiReach(),
    checkTokenizer(),
    checkSessions(),
    checkHooks(projectRoot),
    checkOllama(projectRoot),
    checkProject(projectRoot),
  ]);
}

function checkProxy(): Check {
  const url = detectProxyUrl();
  if (!url) {
    return {
      id: "proxy",
      label: "http proxy   ",
      level: "ok",
      detail: "no HTTPS_PROXY / HTTP_PROXY / ALL_PROXY set — direct connection",
    };
  }
  let redacted = url;
  try {
    const u = new URL(url);
    if (u.username || u.password) {
      u.username = "***";
      u.password = "";
      redacted = u.toString();
    }
  } catch {
    /* not a URL — leave raw */
  }
  return {
    id: "proxy",
    label: "http proxy   ",
    level: "ok",
    detail: `routing fetch through ${redacted}`,
  };
}

const TTY = process.stdout.isTTY && process.env.TERM !== "dumb";

function color(text: string, code: string): string {
  if (!TTY) return text;
  return `\x1b[${code}m${text}\x1b[0m`;
}

function badge(level: Level): string {
  if (level === "ok") return color("✓", "32");
  if (level === "warn") return color("⚠", "33");
  return color("✗", "31");
}

function tail4(s: string): string {
  return s.length <= 4 ? s : `…${s.slice(-4)}`;
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

async function checkApiKey(): Promise<Check> {
  const fromEnv = process.env.DEEPSEEK_API_KEY;
  if (fromEnv) {
    return {
      id: "api-key",
      label: "api key      ",
      level: "ok",
      detail: `set via env DEEPSEEK_API_KEY (${tail4(fromEnv)})`,
    };
  }
  try {
    const cfg = readConfig();
    if (cfg.apiKey) {
      return {
        id: "api-key",
        label: "api key      ",
        level: "ok",
        detail: `from ${defaultConfigPath()} (${tail4(cfg.apiKey)})`,
      };
    }
  } catch {
    /* fall through */
  }
  return {
    id: "api-key",
    label: "api key      ",
    level: "fail",
    detail:
      "not set — `reasonix setup` to save one, or export DEEPSEEK_API_KEY. Get a key at https://platform.deepseek.com/api_keys",
  };
}

async function checkConfig(): Promise<Check> {
  const path = defaultConfigPath();
  if (!existsSync(path)) {
    return {
      id: "config",
      label: "config       ",
      level: "warn",
      detail: "missing — running with library defaults. `reasonix setup` writes one.",
    };
  }
  try {
    const cfg = readConfig(path);
    const parts: string[] = [];
    if (cfg.preset) parts.push(`preset=${cfg.preset}`);
    if (cfg.editMode) parts.push(`editMode=${cfg.editMode}`);
    if (cfg.mcp && cfg.mcp.length > 0) parts.push(`mcp=${cfg.mcp.length}`);
    return {
      id: "config",
      label: "config       ",
      level: "ok",
      detail: `${path}${parts.length ? ` (${parts.join(", ")})` : ""}`,
    };
  } catch (err) {
    return {
      id: "config",
      label: "config       ",
      level: "fail",
      detail: t("doctorErrors.unreadable", { path, message: (err as Error).message }),
    };
  }
}

async function checkApiReach(): Promise<Check> {
  const key = process.env.DEEPSEEK_API_KEY ?? readConfig().apiKey;
  if (!key) {
    return {
      id: "api-reach",
      label: "api reach    ",
      level: "warn",
      detail: "skipped — no api key to test with",
    };
  }
  try {
    const client = new DeepSeekClient({ apiKey: key, baseUrl: loadBaseUrl() });
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), 8_000);
    let balance: Awaited<ReturnType<DeepSeekClient["getBalance"]>>;
    try {
      balance = await client.getBalance({ signal: ctl.signal });
    } finally {
      clearTimeout(timer);
    }
    if (!balance) {
      return {
        id: "api-reach",
        label: "api reach    ",
        level: "fail",
        detail: "/user/balance returned null — auth failed or network blocked",
      };
    }
    const summary = summarizeBalances(balance.balance_infos);
    if (!balance.is_available) {
      return {
        id: "api-reach",
        label: "api reach    ",
        level: "warn",
        detail: `account flagged not-available${summary ? ` (${summary})` : ""} — top up or check your dashboard`,
      };
    }
    return {
      id: "api-reach",
      label: "api reach    ",
      level: "ok",
      detail: summary ? `/user/balance ok — ${summary}` : "/user/balance ok",
    };
  } catch (err) {
    return {
      id: "api-reach",
      label: "api reach    ",
      level: "fail",
      detail: `${(err as Error).message}`,
    };
  }
}

function summarizeBalances(
  infos: ReadonlyArray<{ currency: string; total_balance: string }>,
): string {
  if (infos.length === 0) return "";
  const primary = pickPrimaryBalance(infos);
  if (infos.length === 1 || !primary)
    return primary ? `${primary.total_balance} ${primary.currency}` : "";
  const rest = infos.filter((i) => i !== primary).map((i) => `${i.total_balance} ${i.currency}`);
  return `${primary.total_balance} ${primary.currency} + ${rest.join(" + ")}`;
}

async function checkTokenizer(): Promise<Check> {
  // Reuse the runtime's resolver so the doctor never disagrees with what
  // the tokenizer actually loads — three candidates including a global
  // npm install probe via createRequire.
  const path = resolveDataPath();
  if (existsSync(path)) {
    try {
      const stat = statSync(path);
      return {
        id: "tokenizer",
        label: "tokenizer    ",
        level: "ok",
        detail: `${path} (${fmtBytes(stat.size)})`,
      };
    } catch {
      /* fall through to warn */
    }
  }
  return {
    id: "tokenizer",
    label: "tokenizer    ",
    level: "warn",
    detail:
      "data/deepseek-tokenizer.json.gz not found — token counts will fall back to char heuristics",
  };
}

async function checkSessions(): Promise<Check> {
  try {
    const list = listSessions();
    if (list.length === 0) {
      return {
        id: "sessions",
        label: "sessions     ",
        level: "ok",
        detail: "0 saved",
      };
    }
    const totalBytes = list.reduce((s, e) => s + e.size, 0);
    const oldest = list[list.length - 1]!;
    const ageDays = Math.floor((Date.now() - oldest.mtime.getTime()) / (24 * 60 * 60 * 1000));
    const stale = list.filter(
      (e) => Date.now() - e.mtime.getTime() >= 90 * 24 * 60 * 60 * 1000,
    ).length;
    const detail = `${list.length} saved · ${fmtBytes(totalBytes)} · oldest ${ageDays}d`;
    if (stale > 0) {
      return {
        id: "sessions",
        label: "sessions     ",
        level: "warn",
        detail: `${detail} · ${stale} idle ≥90d (run \`reasonix prune-sessions\`)`,
      };
    }
    return { id: "sessions", label: "sessions     ", level: "ok", detail };
  } catch (err) {
    return {
      id: "sessions",
      label: "sessions     ",
      level: "warn",
      detail: t("doctorErrors.cannotList", { message: (err as Error).message }),
    };
  }
}

async function checkHooks(projectRoot: string): Promise<Check> {
  try {
    const all = loadHooks({ projectRoot });
    const global = all.filter((h) => h.scope === "global").length;
    const project = all.filter((h) => h.scope === "project").length;
    return {
      id: "hooks",
      label: "hooks        ",
      level: "ok",
      detail: `${global} global, ${project} project`,
    };
  } catch (err) {
    return {
      id: "hooks",
      label: "hooks        ",
      level: "warn",
      detail: t("doctorErrors.parseFailed", { message: (err as Error).message }),
    };
  }
}

async function checkOllama(projectRoot: string): Promise<Check> {
  let exists = false;
  try {
    exists = await indexExists(projectRoot);
  } catch {
    /* treat as no index */
  }
  if (!exists) {
    return {
      id: "semantic",
      label: "semantic     ",
      level: "ok",
      detail: "not in use (no semantic index built; `reasonix index` to enable)",
    };
  }
  const meta = readSemanticMeta(projectRoot);
  if (meta?.provider === "openai-compat") {
    const resolved = resolveSemanticEmbeddingConfig();
    if (resolved.provider !== "openai-compat") {
      return {
        id: "semantic",
        label: "semantic     ",
        level: "warn",
        detail: `index uses openai-compat/${meta.model} but current config resolves to ${resolved.provider}/${resolved.model} — rebuild before searching`,
      };
    }
    return {
      id: "semantic",
      label: "semantic     ",
      level: "ok",
      detail: `openai-compat · ${resolved.baseUrl} · model ${resolved.model} · api key configured`,
    };
  }
  try {
    const model = meta?.model || process.env.REASONIX_EMBED_MODEL || "nomic-embed-text";
    const status = await checkOllamaStatus(model);
    if (!status.binaryFound) {
      return {
        id: "semantic",
        label: "semantic     ",
        level: "warn",
        detail:
          "ollama binary not on PATH — semantic_search will fail; install from https://ollama.com",
      };
    }
    if (!status.daemonRunning) {
      return {
        id: "semantic",
        label: "semantic     ",
        level: "warn",
        detail:
          "ollama daemon not running — `ollama serve` (or call /semantic in TUI to auto-start)",
      };
    }
    if (!status.modelPulled) {
      return {
        id: "semantic",
        label: "semantic     ",
        level: "warn",
        detail: `model ${status.modelName} not pulled — \`ollama pull ${status.modelName}\``,
      };
    }
    return {
      id: "semantic",
      label: "semantic     ",
      level: "ok",
      detail: `ollama daemon up · model ${status.modelName} ready`,
    };
  } catch (err) {
    return {
      id: "semantic",
      label: "semantic     ",
      level: "warn",
      detail: t("doctorErrors.probeFailed", { message: (err as Error).message }),
    };
  }
}

function readSemanticMeta(
  projectRoot: string,
): { provider: "ollama" | "openai-compat"; model: string } | null {
  try {
    const raw = readFileSync(join(projectRoot, ".reasonix", "semantic", "index.meta.json"), "utf8");
    const parsed = JSON.parse(raw) as { provider?: string; model?: string };
    return {
      provider: parsed.provider === "openai-compat" ? "openai-compat" : "ollama",
      model: typeof parsed.model === "string" ? parsed.model : "",
    };
  } catch {
    return null;
  }
}

async function checkProject(projectRoot: string): Promise<Check> {
  // Heuristic: a "real" project has either .git, REASONIX.md, or
  // package.json. Lacking all three, `reasonix code` still works but
  // @-mentions and the project-memory pin won't surface much.
  const markers = [".git", "REASONIX.md", "package.json", "pyproject.toml", "Cargo.toml", "go.mod"];
  const found = markers.filter((m) => existsSync(join(projectRoot, m)));
  if (found.length === 0) {
    return {
      id: "project",
      label: "project      ",
      level: "warn",
      detail: `${projectRoot} has none of: ${markers.slice(0, 3).join(", ")} … — \`reasonix code\` will still run, but @-mentions and project memory have nothing to anchor`,
    };
  }
  return {
    id: "project",
    label: "project      ",
    level: "ok",
    detail: `${projectRoot} (${found.join(", ")})`,
  };
}

export function formatDoctorJson(checks: DoctorCheck[], version: string): string {
  const ok = checks.filter((c) => c.level === "ok").length;
  const warn = checks.filter((c) => c.level === "warn").length;
  const fail = checks.filter((c) => c.level === "fail").length;
  return JSON.stringify({
    version,
    summary: { ok, warn, fail },
    checks: checks.map((c) => ({ id: c.id, status: c.level, message: c.detail })),
  });
}

export async function doctorCommand(opts: DoctorOptions = {}): Promise<void> {
  loadDotenv();

  const projectRoot = resolve(process.cwd());
  const json = !!opts.json;

  if (!json) {
    console.log(`${color(`reasonix ${VERSION}  ·  doctor`, "1")}  (cwd: ${projectRoot})`);
    console.log(`  home: ${homedir()}`);
    console.log("");
  }

  // Run independent checks in parallel — saves ~5s when api-reach has
  // to time out. Each handler swallows its own throws into a `fail`
  // result so a thrown promise can't kill the whole report.
  const checks = await runDoctorChecks(projectRoot);

  const ok = checks.filter((c) => c.level === "ok").length;
  const warn = checks.filter((c) => c.level === "warn").length;
  const fail = checks.filter((c) => c.level === "fail").length;

  if (json) {
    console.log(formatDoctorJson(checks, VERSION));
    if (fail > 0) process.exit(1);
    return;
  }

  for (const c of checks) {
    console.log(`  ${badge(c.level)}  ${c.label}  ${c.detail}`);
  }

  console.log("");
  const summary = `${ok} ok · ${warn} warn · ${fail} fail`;
  if (fail > 0) {
    console.log(color(summary, "31"));
    process.exit(1);
  } else if (warn > 0) {
    console.log(color(summary, "33"));
  } else {
    console.log(color(summary, "32"));
  }
}
