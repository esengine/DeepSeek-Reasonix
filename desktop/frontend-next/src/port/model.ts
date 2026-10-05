// Models the kernel can reach, what each costs, and which of them the roles
// beside the main one are pointed at.
// One row of the composer's completion menu. insert is applied verbatim over
// the span the Completion names — the kernel decides what a "/" or "@" means,
// including the escaping a path with spaces needs, so the client never parses
// the line itself.
export interface CompletionItem {
  label: string;
  insert: string;
  hint?: string;
  // A directory, or a command with its own argument menu: accepting re-opens
  // the menu one level deeper instead of closing it.
  descend?: boolean;
  // builtin | command | skill | subagent | prompt | file | dir | resource.
  // Empty on argument values, which are not things.
  kind?: string;
}

export interface Completion {
  kind: "" | "slash" | "slash-arg" | "ref";
  from: number;
  to: number;
  // What the kernel filtered on, so a row can point at the letters that put it
  // here instead of leaving a fuzzy hit unexplained.
  query?: string;
  items: CompletionItem[];
}

export interface ModelPrice {
  input: number;
  output: number;
  cacheHit?: number;
  currency?: string;
}

export interface ModelEntry {
  ref: string;
  provider: string;
  // What the user renamed provider to; provider stays the identity.
  displayName?: string;
  model: string;
  kind?: string;
  active?: boolean;
  default?: boolean;
  // The endpoint host. Rows sharing it are one service reached under more than
  // one protocol, which is what lets the list fold them into a single choice.
  vendor?: string;
  // What the kernel will actually do with this model. An absent field means
  // nothing declares it — never "no". Rendering a guess here sends the user to
  // a rejected request they cannot explain.
  vision?: boolean;
  // What this model produces. Absent means chat: it is what every model was
  // before decision backends, so a kernel predating the field still answers.
  answers?: "chat" | "decision";
  efforts?: string[];
  effort?: string;
  // Even the lowest effort still reasons and is billed.
  forcesThinking?: boolean;
  contextWindow?: number;
  price?: ModelPrice;
  // The credential this route spends; pairs with vendor to name the account.
  keyEnv?: string;
  // Whether provider is a name we shipped. A name the user chose outranks the
  // host when the account is labelled; one of ours does not.
  preset?: boolean;
}

// An optional way the session's model can run, beside its effort level. The
// kernel lists one only for a model that declares it; label and hint are keys
// the frontend owns the wording for.
export interface ModelMode {
  id: string;
  labelKey: string;
  hintKey: string;
  costlier: boolean;
  active: boolean;
}

// A configured provider as the settings panel lists it.
// Which model takes which job. Only the roles with a field behind them in the
// kernel appear here; image routing joins once it can name its own model
// instead of borrowing whatever the subagent runs.
export interface RoleAssignments {
  planner: string;
  subagent: string;
  guardian: string;
  vision: string;
  // The system_one backend. It names a model on a decision wire, so unlike the
  // others it never falls back to the main model: a chat model cannot answer a
  // question set, and a silent fallback would make a missing backend a wrong one.
  decision: string;
}

// The two fold bounds and which of them is in force. They are configured
// separately and only the lower one ever fires, so a panel showing the
// settings alone cannot say which: a 1M window against the default soft limit
// folds at 160k, and nothing on that screen would explain it.
export interface CompactionSettings {
  // The stored value, not the resolved one: 0 means the default, negative
  // means off, and a caller that cannot tell those apart cannot offer them.
  soft_limit_tokens: number;
  default_soft_limit: number;
  ratio: number;
  // Zero when nobody declared a window, which is also what turns automatic
  // maintenance off entirely.
  context_window: number;
  trigger: number;
  path: string;
}
