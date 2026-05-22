export type LifecyclePolicyRisk = "safe" | "mutation" | "high-risk";

export type LifecyclePolicyReason =
  | "safe-tool"
  | "mutation-tool"
  | "high-risk-tool"
  | "package-or-config-path"
  | "high-risk-command"
  | "default-safe";

export interface LifecyclePolicyDecision {
  toolName: string;
  risk: LifecyclePolicyRisk;
  reason: LifecyclePolicyReason;
}

const SAFE_TOOL_NAMES = new Set([
  "read_file",
  "list_directory",
  "directory_tree",
  "search_files",
  "search_content",
  "glob",
  "get_file_info",
  "semantic_search",
  "web_search",
  "web_fetch",
  "recall_memory",
  "todo_write",
  "ask_choice",
  "submit_plan",
  "mark_step_complete",
  "revise_plan",
  "job_output",
  "wait_for_job",
  "list_jobs",
]);

const HIGH_RISK_TOOL_NAMES = new Set([
  "multi_edit",
  "move_file",
  "delete_file",
  "delete_directory",
  "copy_file",
  "create_directory",
  "run_background",
  "stop_job",
]);

const MUTATION_TOOL_NAMES = new Set([
  "edit_file",
  "write_file",
  "multi_edit",
  "move_file",
  "delete_file",
  "delete_directory",
  "copy_file",
  "create_directory",
  "run_background",
  "stop_job",
]);

export function classifyLifecycleToolCall(
  toolName: string,
  args: Record<string, unknown>,
): LifecyclePolicyDecision {
  if (HIGH_RISK_TOOL_NAMES.has(toolName)) {
    return decision(toolName, "high-risk", "high-risk-tool");
  }
  if (SAFE_TOOL_NAMES.has(toolName)) {
    return decision(toolName, "safe", "safe-tool");
  }
  if (toolName === "write_file" || toolName === "edit_file") {
    const path = typeof args.path === "string" ? args.path : "";
    if (isPackageOrConfigPath(path)) {
      return decision(toolName, "high-risk", "package-or-config-path");
    }
    return decision(toolName, "mutation", "mutation-tool");
  }
  if (toolName === "run_command") {
    const command = typeof args.command === "string" ? args.command : "";
    if (isHighRiskCommand(command)) {
      return decision(toolName, "high-risk", "high-risk-command");
    }
    return decision(toolName, "safe", "default-safe");
  }
  if (MUTATION_TOOL_NAMES.has(toolName)) {
    return decision(toolName, "mutation", "mutation-tool");
  }
  return decision(toolName, "safe", "default-safe");
}

function decision(
  toolName: string,
  risk: LifecyclePolicyRisk,
  reason: LifecyclePolicyReason,
): LifecyclePolicyDecision {
  return { toolName, risk, reason };
}

function isPackageOrConfigPath(path: string): boolean {
  const normalized = path.replaceAll("\\", "/").toLowerCase();
  return (
    /(^|\/)package(-lock)?\.json$/.test(normalized) ||
    /(^|\/)pnpm-lock\.yaml$/.test(normalized) ||
    /(^|\/)yarn\.lock$/.test(normalized) ||
    /(^|\/)tsconfig[^/]*\.json$/.test(normalized) ||
    /(^|\/)vitest\.config\./.test(normalized) ||
    /(^|\/)biome\.json$/.test(normalized) ||
    normalized.startsWith(".github/workflows/")
  );
}

function isHighRiskCommand(command: string): boolean {
  const tokens = shellTokens(command);
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i]?.toLowerCase();
    if (!token || !isCommandPosition(tokens, i)) continue;
    if (
      (token === "npm" || token === "pnpm" || token === "yarn") &&
      isPackageMutation(tokens[i + 1])
    ) {
      return true;
    }
    if (token === "git" && isHighRiskGitCommand(tokens.slice(i + 1))) return true;
    if (token === "rm" || token === "mv" || token === "cp") return true;
  }
  return false;
}

function shellTokens(command: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote: "'" | '"' | null = null;
  for (let i = 0; i < command.length; i++) {
    const ch = command[i] ?? "";
    if (quote) {
      if (ch === quote) quote = null;
      else current += ch;
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      continue;
    }
    if (/\s/.test(ch)) {
      if (current) {
        out.push(current);
        current = "";
      }
      continue;
    }
    if (ch === ";" || ch === "|" || ch === "&") {
      if (current) {
        out.push(current);
        current = "";
      }
      const next = command[i + 1];
      if ((ch === "|" || ch === "&") && next === ch) {
        out.push(`${ch}${next}`);
        i++;
      } else {
        out.push(ch);
      }
      continue;
    }
    current += ch;
  }
  if (current) out.push(current);
  return out;
}

function isCommandPosition(tokens: string[], index: number): boolean {
  if (index === 0) return true;
  const previous = tokens[index - 1];
  return previous === ";" || previous === "|" || previous === "&&" || previous === "||";
}

function isPackageMutation(token: string | undefined): boolean {
  const normalized = token?.toLowerCase();
  return (
    normalized === "install" ||
    normalized === "add" ||
    normalized === "remove" ||
    normalized === "update"
  );
}

function isHighRiskGitCommand(args: string[]): boolean {
  const subcommandIndex = args.findIndex((arg) => arg && !arg.startsWith("-"));
  const subcommand = args[subcommandIndex]?.toLowerCase();
  if (!subcommand) return false;
  if (
    subcommand === "push" ||
    subcommand === "reset" ||
    subcommand === "clean" ||
    subcommand === "switch"
  ) {
    return true;
  }
  if (subcommand !== "checkout") return false;
  const checkoutArgs = args.slice(subcommandIndex + 1);
  if (checkoutArgs[0] === "--") return false;
  if (checkoutArgs.some((arg) => arg === "-b" || arg === "-B" || arg === "--orphan")) return true;
  const positional = checkoutArgs.filter((arg) => arg && !arg.startsWith("-"));
  if (positional.length === 0) return false;
  return !positional.every(looksLikePathCheckout);
}

function looksLikePathCheckout(arg: string): boolean {
  return arg.includes("/") || arg.includes("\\") || arg.includes(".");
}
