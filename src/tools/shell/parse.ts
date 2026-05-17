import { homedir } from "node:os";
import * as pathMod from "node:path";
import { type CommandChain, chainAllowed, parseCommandChain } from "../shell-chain.js";

/** Read-only reports + test runners whose failure mode is "exit 1 with output". */
export const BUILTIN_ALLOWLIST: ReadonlyArray<string> = [
  // Repo inspection
  "git status",
  "git diff",
  "git log",
  "git show",
  "git blame",
  "git branch",
  "git remote",
  "git rev-parse",
  "git config --get",
  // Filesystem inspection
  "ls",
  "pwd",
  "cat",
  "head",
  "tail",
  "wc",
  "file",
  "tree",
  "find",
  "grep",
  "rg",
  // Language version probes
  "node --version",
  "node -v",
  "npm --version",
  "npx --version",
  "python --version",
  "python3 --version",
  "cargo --version",
  "go version",
  "rustc --version",
  "deno --version",
  "bun --version",
  // Test runners (non-destructive by convention)
  "npm test",
  "npm run test",
  "npx vitest run",
  "npx vitest",
  "npx jest",
  "pytest",
  "python -m pytest",
  "cargo test",
  "cargo check",
  "cargo clippy",
  "go test",
  "go vet",
  "deno test",
  "bun test",
  // Linters / typecheckers (read-only by convention)
  "npm run lint",
  "npm run typecheck",
  "npx tsc --noEmit",
  "npx biome check",
  "npx eslint",
  "npx prettier --check",
  "ruff",
  "mypy",
];

/** Inside `"…"` only `\"` and `\\` are escapes — `\X` otherwise stays literal so Windows paths like `"C:\Users\foo\.bar"` survive tokenization. */
export function isDqEscape(prev: string, next: string | undefined): boolean {
  return prev === "\\" && (next === '"' || next === "\\");
}

/** No env / glob / backtick / `$(…)` expansion — prevents bypass of allowlist via concatenation. */
export function tokenizeCommand(cmd: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i]!;
    if (quote) {
      if (ch === quote) {
        quote = null;
      } else if (quote === '"' && isDqEscape(ch, cmd[i + 1])) {
        cur += cmd[++i];
      } else {
        cur += ch;
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === " " || ch === "\t") {
      if (cur.length > 0) {
        out.push(cur);
        cur = "";
      }
      continue;
    }
    cur += ch;
  }
  if (quote) throw new Error(`unclosed ${quote} in command`);
  if (cur.length > 0) out.push(cur);
  return out;
}

/** Up-front detection — without it, `dir | findstr foo` quotes `|` literal and pipe silently fails. */
export function detectShellOperator(cmd: string): string | null {
  const opPrefix = /^(?:2>&1|&>|\|{1,2}|&{1,2}|2>{1,2}|>{1,2}|<{1,2})/;
  let cur = "";
  let curQuoted = false;
  let quote: '"' | "'" | null = null;
  const check = (): string | null => {
    if (cur.length === 0 && !curQuoted) return null;
    if (!curQuoted) {
      const m = opPrefix.exec(cur);
      if (m) return m[0] ?? null;
    }
    return null;
  };
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i]!;
    if (quote) {
      if (ch === quote) {
        quote = null;
      } else if (quote === '"' && isDqEscape(ch, cmd[i + 1])) {
        cur += cmd[++i];
        curQuoted = true;
      } else {
        cur += ch;
        curQuoted = true;
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      curQuoted = true;
      continue;
    }
    if (ch === " " || ch === "\t") {
      const op = check();
      if (op) return op;
      cur = "";
      curQuoted = false;
      continue;
    }
    cur += ch;
  }
  if (quote) return null; // let tokenizeCommand throw the unclosed-quote error
  return check();
}

/** Per-prefix demotion: an otherwise-allowlisted match falls back to the confirm gate when one of these tokens appears in the tail. Issue #257: `git branch -D` skipped review. Each token also matches its `--flag=value` form. */
const RISKY_ARGS: Readonly<Record<string, ReadonlyArray<string>>> = {
  // Branch / remote mutation
  "git branch": ["-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy", "--force"],
  "git remote": ["add", "remove", "rm", "rename", "set-url", "set-head", "prune"],
  // `--output` writes to an arbitrary path; `--ext-diff` invokes user-config'd external programs.
  "git diff": ["--output", "--ext-diff"],
  "git log": ["--output"],
  "git show": ["--output"],
  // `-exec*` / `-ok*` are RCE; `-delete` and `-fprint*` / `-fls` write to arbitrary paths.
  find: [
    "-delete",
    "-exec",
    "-execdir",
    "-ok",
    "-okdir",
    "-fprint",
    "-fprint0",
    "-fprintf",
    "-fls",
  ],
  // `-o FILE` writes the tree to an arbitrary path.
  tree: ["-o"],
  // Auto-fix mutates source files.
  "npx eslint": ["--fix", "--fix-dry-run"],
  "npx biome check": ["--write", "--apply", "--apply-unsafe"],
  ruff: ["--fix", "--unsafe-fixes", "format"],
};

function tailHasRisky(tail: readonly string[], risky: readonly string[]): boolean {
  for (const a of tail) {
    for (const r of risky) {
      if (a === r) return true;
      if (a.startsWith(`${r}=`)) return true;
    }
  }
  return false;
}

/** Issue #259 — default sensitive-path prefixes (tilde-relative). Matching a path argument against these
 *  demotes an otherwise-allowlisted command back to the confirm gate, preventing the agent from
 *  silently reading credentials / keys and piping them into the LLM context. */
const DEFAULT_SENSITIVE_PREFIXES: ReadonlyArray<string> = [
  "~/.ssh",
  "~/.aws",
  "~/.gnupg",
  "~/.kube",
  "/etc/shadow",
  "/etc/sudoers",
];

/** Issue #259 — filename patterns (case-insensitive basename match). */
const DEFAULT_SENSITIVE_PATTERNS: ReadonlyArray<string> = [
  "*.env",
  "*.env.*",
  "*.key",
  "*.pem",
  "id_rsa*",
  "id_ed25519*",
  "*credentials*",
  "*secret*",
];

/** Resolve `~` to `homedir()` and normalize. Non-path-like tokens (flags, URLs, env vars) are skipped. */
function resolveSensitivePath(token: string, projectRoot: string): string | null {
  if (!token || token.startsWith("-") || token.includes("://") || token.startsWith("$"))
    return null;
  let expanded = token;
  if (expanded.startsWith("~")) {
    expanded = pathMod.join(homedir(), expanded.slice(1));
  }
  return pathMod.resolve(projectRoot, expanded);
}

function expandPrefix(prefix: string): string {
  if (prefix.startsWith("~")) return pathMod.join(homedir(), prefix.slice(1));
  return pathMod.resolve(prefix);
}

/** Ensure prefix matches only at directory boundaries (not mid-segment). */
function pathStartsWithPrefix(normalized: string, prefix: string): boolean {
  return normalized === prefix || normalized.startsWith(`${prefix}${pathMod.sep}`);
}

/** Glob-style match: `*.env` matches `foo.env`, `id_rsa*` matches `id_rsa_old`. */
function matchesGlob(name: string, pattern: string): boolean {
  const regex = new RegExp(
    `^${pattern
      .replace(/[.+^${}()|[\]\\]/g, "\\$&")
      .replace(/\*/g, ".*")
      .replace(/\?/g, ".")}$`,
    "i",
  );
  return regex.test(name);
}

/** Check whether a command's path-like arguments touch sensitive locations. */
export function hasSensitivePathArgs(
  argv: readonly string[],
  projectRoot: string,
  extraPrefixes: readonly string[] = [],
  extraPatterns: readonly string[] = [],
): boolean {
  const prefixes = [...DEFAULT_SENSITIVE_PREFIXES, ...extraPrefixes].map(expandPrefix);
  const patterns = [...DEFAULT_SENSITIVE_PATTERNS, ...extraPatterns];
  for (const token of argv) {
    const resolved = resolveSensitivePath(token, projectRoot);
    if (!resolved) continue;
    const normalized = pathMod.normalize(resolved);
    for (const pfx of prefixes) {
      if (pathStartsWithPrefix(normalized, pfx)) return true;
    }
    const base = pathMod.basename(normalized);
    for (const pat of patterns) {
      if (matchesGlob(base, pat)) return true;
    }
  }
  return false;
}

/** Allowlist match on leading argv tokens; demoted by `RISKY_ARGS` when a destructive flag appears in the tail,
 *  or by `SENSITIVE_PATHS` when a path argument targets a sensitive location (#259). */
export function isAllowed(
  cmd: string,
  extra: readonly string[] = [],
  projectRoot?: string,
  sensitivePathConfig?: { prefixes?: readonly string[]; patterns?: readonly string[] },
): boolean {
  let argv: string[];
  try {
    argv = tokenizeCommand(cmd);
  } catch {
    return false;
  }
  if (argv.length === 0) return false;

  const allowlist = [...BUILTIN_ALLOWLIST, ...extra];
  for (const prefix of allowlist) {
    const prefixTokens = prefix.split(" ");
    if (argv.length < prefixTokens.length) continue;
    let match = true;
    for (let i = 0; i < prefixTokens.length; i++) {
      if (argv[i] !== prefixTokens[i]) {
        match = false;
        break;
      }
    }
    if (!match) continue;

    const risky = RISKY_ARGS[prefix];
    if (risky && tailHasRisky(argv.slice(prefixTokens.length), risky)) return false;
    if (
      projectRoot &&
      hasSensitivePathArgs(
        argv,
        projectRoot,
        sensitivePathConfig?.prefixes,
        sensitivePathConfig?.patterns,
      )
    )
      return false;
    return true;
  }
  return false;
}

/** For chain commands, every segment must individually clear the allowlist. */
export function isCommandAllowed(
  cmd: string,
  extra: readonly string[] = [],
  projectRoot?: string,
  sensitivePathConfig?: { prefixes?: readonly string[]; patterns?: readonly string[] },
): boolean {
  let chain: CommandChain | null;
  try {
    chain = parseCommandChain(cmd);
  } catch {
    return false;
  }
  if (chain === null) return isAllowed(cmd, extra, projectRoot, sensitivePathConfig);
  return chainAllowed(chain, (seg) => isAllowed(seg, extra, projectRoot, sensitivePathConfig));
}
