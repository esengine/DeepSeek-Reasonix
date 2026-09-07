// Endpoint literals, and the adjudication that decides whether reaching one
// changes canonical host state.
import { SRC, trees } from "./source.mjs";

// The path a request names, when it is written from literals: a string, or a
// `+` chain whose unreadable parts are the origin. A module-level string const
// resolves; anything else leaves that part unknown and contributes nothing.
const moduleStrings = new Map();
for (const [path, tree] of trees) {
  const m = new Map();
  for (const st of tree.program.body) {
    const d = st.type === "VariableDeclaration" ? st : st.type === "ExportNamedDeclaration" ? st.declaration : null;
    if (d?.type !== "VariableDeclaration") continue;
    for (const v of d.declarations) {
      if (v.id?.type === "Identifier" && v.init?.type === "StringLiteral") m.set(v.id.name, v.init.value);
    }
  }
  moduleStrings.set(path, m);
}
// Every path an expression can name. A control that branches between two
// endpoints — `paused ? "/inbox/pause" : "/inbox/resume"` — names both, and
// reading only one of them made the function look like it was forwarding a
// path it was in fact supplying.
const endpointsOf = (n, path) => {
  const parts = (x) => {
    if (!x) return [[null]];
    if (x.type === "StringLiteral") return [[x.value]];
    if (x.type === "Identifier") return [[moduleStrings.get(path)?.get(x.name) ?? null]];
    if (x.type === "TemplateLiteral") {
      const out = [];
      (x.quasis ?? []).forEach((q, i) => {
        out.push(q.value?.cooked ?? "");
        if (i < (x.expressions ?? []).length) out.push(null);
      });
      return [out];
    }
    if (x.type === "BinaryExpression" && x.operator === "+") {
      return parts(x.left).flatMap((l) => parts(x.right).map((r) => [...l, ...r]));
    }
    if (x.type === "ConditionalExpression") return [...parts(x.consequent), ...parts(x.alternate)];
    if (x.type === "LogicalExpression") return [...parts(x.left), ...parts(x.right)];
    if (x.type === "TSAsExpression" || x.type === "TSNonNullExpression") return parts(x.expression);
    return [[null]];
  };
  const out = [];
  for (const combo of parts(n)) {
    const named = combo.filter((x) => x !== null && x !== "").join("");
    if (named.startsWith("/")) out.push(named);
  }
  return [...new Set(out)];
};
const endpointOf = (n, path) => endpointsOf(n, path)[0] ?? null;

// A mutating verb is the transport's fact. Whether the request changes host
// state is the endpoint's, and these two answers can differ: one branch of a
// port method sends a POST for what the other branch gets for free from its
// GET. Declared per endpoint, against the host operation it actually reaches,
// and closed — an entry that excuses nothing fails, so a renamed path cannot
// leave a silent exemption standing.
// Adjudicated per endpoint, and an endpoint is a verb and a path: the same
// path can answer a different question under POST and under DELETE, and one
// declaration must not wash the other out. Declared per tree, and closed
// against the tree it belongs to — an entry that excuses nothing fails.
const DECLARED_NON_MUTATION = {
  src: {
    "POST /providers/probe": { class: "repeatable-external-probe", why:
      "config.ProbeEndpoint asks the endpoint what models it has and the answer is written back to the " +
      "caller; no canonical host state is written. External network effect: yes — this set decides " +
      "canonical host state, not whether anything happens at all." },
    "POST /providers/check": { class: "repeatable-external-probe", why:
      "the same host operation as /providers/probe, reached under a saved name. " +
      "internal/serve/provider_check.go's checkProvider reads config.Load, looks the entry up and calls " +
      "the same config.ProbeEndpoint; the answer is written back to the caller and no canonical host " +
      "state is written. External network effect: yes." },
    "POST /mcp/parse": { class: "pure-parse", why:
      "internal/serve/catalog.go's mcpParse decodes the body, calls mcpsetup.Parse, assembles servers and " +
      "risks and writes them back; internal/mcpsetup performs no write at all — no file, no store." },
    "POST /rx-replay": { class: "attach-handshake", why:
      "A bus subscription is not a request and cannot carry one, so the bus branch sends this; " +
      "desktop/next/main.go answers it with Controller.ReplayPendingPromptsWith — a read of the pending " +
      "prompts and an emit to the sink just attached, which is the same host operation GET /events " +
      "performs inline on the EventSource branch (internal/serve/eventstream.go)." },
  },
  // The fixture tree declares its own, so the rule is exercised rather than the
  // product's vocabulary being borrowed into a test.
  _fx: {
    "POST /probe": { class: "fixture-probe", why: "T2: adjudicated through a wrapper" },
    "POST /probe2": { class: "fixture-probe", why: "T7: one endpoint of a wrapper two endpoints share" },
    "POST /same": { class: "fixture-probe", why: "T8: one verb of a path two verbs share" },
  },
};

// Two endpoints the host answers with one operation. Named so that deleting
// either adjudication fails here rather than silently making one of a pair
// mutating: nothing about a shared path prefix says they are the same question,
// and nothing about a shared class exempts either from its own declaration or
// from having to match a live EndpointSite.
const SEMANTIC_PAIRS = {
  src: [["POST /providers/probe", "POST /providers/check"]],
  _fx: [],
};

const NOT_A_MUTATION = DECLARED_NON_MUTATION[SRC] ?? {};
const excused = [];

export { DECLARED_NON_MUTATION, NOT_A_MUTATION, SEMANTIC_PAIRS, endpointOf, endpointsOf, excused, moduleStrings };
