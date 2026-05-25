type Platform = NodeJS.Platform;

/** Legacy `powershell.exe` / `cmd.exe` running under conhost; it repaints each Ink frame visibly. */
export function isLegacyWindowsConsole(
  env: NodeJS.ProcessEnv = process.env,
  platform: Platform = process.platform,
): boolean {
  return platform === "win32" && !env.WT_SESSION && !env.TERM_PROGRAM;
}

// Terminals that visibly repaint rapid Ink frames during streaming output.
// Windows Terminal also hosts WSL sessions, so WSL markers matter on Linux too.
export function prefersReducedTerminalPaint(
  env: NodeJS.ProcessEnv = process.env,
  platform: Platform = process.platform,
): boolean {
  return (
    isLegacyWindowsConsole(env, platform) ||
    Boolean(env.WT_SESSION) ||
    Boolean(env.WSL_DISTRO_NAME) ||
    Boolean(env.WSL_INTEROP)
  );
}

export function terminalFlushIntervalMs(
  env: NodeJS.ProcessEnv = process.env,
  platform: Platform = process.platform,
): number {
  const fallback = prefersReducedTerminalPaint(env, platform) ? 150 : 50;
  const raw = env.REASONIX_FLUSH_MS;
  if (!raw) return fallback;
  const parsed = Number(raw);
  if (!Number.isFinite(parsed) || parsed < 16 || parsed > 1000) return fallback;
  return Math.round(parsed);
}
