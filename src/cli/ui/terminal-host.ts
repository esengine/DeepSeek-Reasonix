type Platform = NodeJS.Platform;

/** Legacy `powershell.exe` / `cmd.exe` running under conhost; it repaints each Ink frame visibly. */
export function isLegacyWindowsConsole(
  env: NodeJS.ProcessEnv = process.env,
  platform: Platform = process.platform,
): boolean {
  return platform === "win32" && !env.WT_SESSION && !env.TERM_PROGRAM;
}
