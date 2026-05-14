/** Browser extensions (userscripts, ad blockers) inject scripts that throw into our window. Their errors land in the global handlers as if they were ours — see #818. Filter them by origin. */
const THIRD_PARTY_ORIGIN_PREFIXES = [
  "chrome-extension://",
  "moz-extension://",
  "safari-web-extension://",
  "safari-extension://",
  "ms-browser-extension://",
];

export function isThirdPartyError(error: unknown, filename?: string): boolean {
  const hay = `${filename ?? ""}\n${(error as { stack?: string } | null)?.stack ?? ""}`;
  return THIRD_PARTY_ORIGIN_PREFIXES.some((prefix) => hay.includes(prefix));
}
