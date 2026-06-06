/** First two tokens for known wrappers (`npm install`, `git commit`, …); else first token only.
 *  Used by the "always allow" permission gate so that `npm test` and `npm run build`
 *  share one prefix, while `cargo` and `ls` stay single-token.
 *
 *  This function is duplicated in ≥2 build targets (CLI + Desktop), so it lives
 *  in `@reasonix/core-utils` to prevent drift like issue #1180. */
export function derivePrefix(command: string): string {
  const tokens = command.trim().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return "";
  if (tokens.length === 1) return tokens[0]!;
  const first = tokens[0]!;
  const TWO_TOKEN_WRAPPERS = new Set([
    "npm",
    "npx",
    "pnpm",
    "yarn",
    "bun",
    "git",
    "cargo",
    "go",
    "docker",
    "kubectl",
    "python",
    "python3",
    "deno",
    "pip",
    "pip3",
    "make",
    "rake",
    "bundle",
    "gem",
  ]);
  return TWO_TOKEN_WRAPPERS.has(first) ? `${first} ${tokens[1]}` : first;
}
