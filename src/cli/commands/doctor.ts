/** Plain-text (not Ink) — must work when everything else is broken. fail → exit 1; warn → exit 0. */

import { existsSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { DeepSeekClient } from "../../client.js";
import { defaultConfigPath, readConfig, resolveSemanticEmbeddingConfig } from "../../config.js";
import { loadDotenv } from "../../env.js";
import { loadHooks } from "../../hooks.js";
import { indexExists } from "../../index/semantic/builder.js";
import { checkOllamaStatus } from "../../index/semantic/ollama-launcher.js";
import { listSessions } from "../../memory/session.js";
import { VERSION } from "../../version.js";

export type DoctorLevel = "ok" | "warn" | "fail";

export interface DoctorCheck {
  label: string;
  level: DoctorLevel;
  detail: string;
}

type Level = DoctorLevel;
type Check = DoctorCheck;

export async function runDoctorChecks(projectRoot: string): Promise<DoctorCheck[]> {
  return Promise.all([
    checkApiKey(),
    checkConfig(),
    checkApiReach(),
    checkTokenizer(),
    checkSessions(),
    checkHooks(projectRoot),
    checkOllama(projectRoot),
    checkProject(projectRoot),
  ]);
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
      label: "api key      ",
      level: "ok",
      detail: `set via env DEEPSEEK_API_KEY (${tail4(fromEnv)})`,
    };
  }
  try {
    const cfg = readConfig();
    if (cfg.apiKey) {
      return {
        label: "api key      ",
        level: "ok",
        detail: `from ${defaultConfigPath()} (${tail4(cfg.apiKey)})`,
      };
    }
  } catch {
    /* fall through */
  }
  return {
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
      label: "config       ",
      level: "ok",
      detail: `${path}${parts.length ? ` (${parts.join(", ")})` : ""}`,
    };
  } catch (err) {
    return {
      label: "config       ",
      level: "fail",
      detail: `${path} unreadable — ${(err as Error).message}`,
    };
  }
}

async function checkApiReach(): Promise<Check> {
  const key = process.env.DEEPSEEK_API_KEY ?? readConfig().apiKey;
  if (!key) {
    return {
      label: "api reach    ",
      level: "warn",
      detail: "skipped — no api key to test with",
    };
  }
  try {
    const client = new DeepSeekClient({ apiKey: key });
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
        label: "api reach    ",
        level: "fail",
        detail: "/user/balance returned null — auth failed or network blocked",
      };
    }
    if (!balance.is_available) {
      const info = balance.balance_infos[0];
      return {
        label: "api reach    ",
        level: "warn",
        detail: `account flagged not-available${info ? ` (${info.total_balance} ${info.currency})` : ""} — top up or check your dashboard`,
      };
    }
    const info = balance.balance_infos[0];
    return {
      label: "api reach    ",
      level: "ok",
      detail: info
        ? `/user/balance ok — ${info.total_balance} ${info.currency}`
        : "/user/balance ok",
    };
  } catch (err) {
    return {
      label: "api reach    ",
      level: "fail",
      detail: `${(err as Error).message}`,
    };
  }
}

async function checkTokenizer(): Promise<Check> {
  // The tokenizer file is the gzipped vocab shipped under data/. We
  // resolve relative to the package's dist/ rather than process.cwd()
  // — the same logic the runtime uses when loading on first count.
  const candidates = [
    join(
      dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1")),
      "..",
      "..",
      "..",
      "data",
      "deepseek-tokenizer.json.gz",
    ),
    join(process.cwd(), "data", "deepseek-tokenizer.json.gz"),
  ];
  for (const p of candidates) {
    if (existsSync(p)) {
      try {
        const stat = statSync(p);
        return {
          label: "tokenizer    ",
          level: "ok",
          detail: `${p} (${fmtBytes(stat.size)})`,
        };
      } catch {
        /* try next */
      }
    }
  }
  return {
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
        label: "sessions     ",
        level: "warn",
        detail: `${detail} · ${stale} idle ≥90d (run \`reasonix prune-sessions\`)`,
      };
    }
    return { label: "sessions     ", level: "ok", detail };
  } catch (err) {
    return {
      label: "sessions     ",
      level: "warn",
      detail: `cannot list — ${(err as Error).message}`,
    };
  }
}

async function checkHooks(projectRoot: string): Promise<Check> {
  try {
    const all = loadHooks({ projectRoot });
    const global = all.filter((h) => h.scope === "global").length;
    const project = all.filter((h) => h.scope === "project").length;
    return {
      label: "hooks        ",
      level: "ok",
      detail: `${global} global, ${project} project`,
    };
  } catch (err) {
    return {
      label: "hooks        ",
      level: "warn",
      detail: `couldn't parse settings.json — ${(err as Error).message}`,
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
        label: "semantic     ",
        level: "warn",
        detail: `index uses openai-compat/${meta.model} but current config resolves to ${resolved.provider}/${resolved.model} — rebuild before searching`,
      };
    }
    return {
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
        label: "semantic     ",
        level: "warn",
        detail:
          "ollama binary not on PATH — semantic_search will fail; install from https://ollama.com",
      };
    }
    if (!status.daemonRunning) {
      return {
        label: "semantic     ",
        level: "warn",
        detail:
          "ollama daemon not running — `ollama serve` (or call /semantic in TUI to auto-start)",
      };
    }
    if (!status.modelPulled) {
      return {
        label: "semantic     ",
        level: "warn",
        detail: `model ${status.modelName} not pulled — \`ollama pull ${status.modelName}\``,
      };
    }
    return {
      label: "semantic     ",
      level: "ok",
      detail: `ollama daemon up · model ${status.modelName} ready`,
    };
  } catch (err) {
    return {
      label: "semantic     ",
      level: "warn",
      detail: `probe failed — ${(err as Error).message}`,
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
      label: "project      ",
      level: "warn",
      detail: `${projectRoot} has none of: ${markers.slice(0, 3).join(", ")} … — \`reasonix code\` will still run, but @-mentions and project memory have nothing to anchor`,
    };
  }
  return {
    label: "project      ",
    level: "ok",
    detail: `${projectRoot} (${found.join(", ")})`,
  };
}

export async function doctorCommand(): Promise<void> {
  loadDotenv();

  const projectRoot = resolve(process.cwd());
  console.log(`${color(`reasonix ${VERSION}  ·  doctor`, "1")}  (cwd: ${projectRoot})`);
  console.log(`  home: ${homedir()}`);
  console.log("");

  // Run independent checks in parallel — saves ~5s when api-reach has
  // to time out. Each handler swallows its own throws into a `fail`
  // result so a thrown promise can't kill the whole report.
  const checks = await runDoctorChecks(projectRoot);

  for (const c of checks) {
    console.log(`  ${badge(c.level)}  ${c.label}  ${c.detail}`);
  }

  const ok = checks.filter((c) => c.level === "ok").length;
  const warn = checks.filter((c) => c.level === "warn").length;
  const fail = checks.filter((c) => c.level === "fail").length;
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
