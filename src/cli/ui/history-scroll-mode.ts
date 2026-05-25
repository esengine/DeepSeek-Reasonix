import type { HistoryScrollMode } from "../../config.js";

export type ResolvedHistoryScrollMode = "native" | "app";

export interface ResolveHistoryScrollModeInput {
  configured?: HistoryScrollMode;
  env?: NodeJS.ProcessEnv | Record<string, string | undefined>;
  platform?: NodeJS.Platform;
}

export function resolveHistoryScrollMode({
  configured = "auto",
  env = process.env,
  platform = process.platform,
}: ResolveHistoryScrollModeInput = {}): ResolvedHistoryScrollMode {
  if (configured === "native") return "native";
  if (configured === "app") return "app";
  if (isKnownJumpProneTerminal(env)) return "app";
  if (platform === "win32" && env.TERM_PROGRAM === undefined && env.MSYSTEM === undefined) {
    return "native";
  }
  return "native";
}

function isKnownJumpProneTerminal(env: NodeJS.ProcessEnv | Record<string, string | undefined>) {
  const termProgram = (env.TERM_PROGRAM ?? "").toLowerCase();
  if (termProgram === "vscode" || termProgram === "ghostty") return true;
  if (typeof env.WT_SESSION === "string" && env.WT_SESSION.length > 0) return true;
  if (typeof env.MSYSTEM === "string" && env.MSYSTEM.length > 0) return true;
  if ((env.TERM ?? "").toLowerCase().includes("xterm-ghostty")) return true;
  return false;
}
