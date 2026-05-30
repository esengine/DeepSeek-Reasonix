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
  // Apple Terminal has native renderer crashes when it receives
  // private mouse-mode toggles — keep it on native so the terminal
  // handles scrollback without any escape sequences.
  if ((env.TERM_PROGRAM ?? "").toLowerCase() === "apple_terminal") return "native";
  if (isKnownJumpProneTerminal(env)) return "app";
  // Classic Windows console (conhost) doesn't advertise TERM_PROGRAM
  // and its alt-screen buffer doesn't forward wheel events — native
  // scrollback is the safer default there.
  if (platform === "win32" && env.TERM_PROGRAM === undefined && env.MSYSTEM === undefined) {
    return "native";
  }
  // Default to app-managed scroll for all other terminals so the mouse
  // wheel feeds into CardStream's scroll logic. Native scrollback
  // cannot scroll TUI alt-screen content on most terminals.
  return "app";
}

function isKnownJumpProneTerminal(env: NodeJS.ProcessEnv | Record<string, string | undefined>) {
  const termProgram = (env.TERM_PROGRAM ?? "").toLowerCase();
  if (termProgram === "vscode" || termProgram === "ghostty") return true;
  if (typeof env.WT_SESSION === "string" && env.WT_SESSION.length > 0) return true;
  if (typeof env.MSYSTEM === "string" && env.MSYSTEM.length > 0) return true;
  if ((env.TERM ?? "").toLowerCase().includes("xterm-ghostty")) return true;
  return false;
}
