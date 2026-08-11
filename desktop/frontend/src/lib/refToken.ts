// refToken mirrors the Go @-token grammar in internal/control/refs.go: a token
// is a run of non-whitespace, except that a backslash-escaped space or tab
// stays inside the token. Any other backslash is literal so Windows separators
// keep their meaning. escapeRefPath/unescapeRefPath are the inverse pair the
// control layer applies when resolving refs on submit.

// One token character: an escaped space/tab pair, or any non-whitespace byte.
const TOKEN_CHAR = String.raw`(?:\\[ \t]|[^\s])`;

// The @token ending at the cursor (token may be empty while still typing).
export const activeRefTokenRe = new RegExp(`(?:^|\\s)@(${TOKEN_CHAR}*)$`);

// All @tokens in a text, for display badge extraction.
export function refTokenRe(): RegExp {
  return new RegExp(`(^|\\s)@(${TOKEN_CHAR}+)`, "g");
}

export function escapeRefPath(path: string): string {
  return path.replace(/[ \t]/g, (ws) => "\\" + ws);
}

export function unescapeRefPath(token: string): string {
  return token.replace(/\\([ \t])/g, "$1");
}
