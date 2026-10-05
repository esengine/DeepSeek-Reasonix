// Platform scheduling calls, with the provenance that makes one.
//
// A scheduler runs the callback it is handed, later. Which is a contract about
// the platform's function, so it may only be read where that function is what
// is actually being called: a bare name nothing nearer binds, or a member call
// on a receiver proven to be the global object. A name is never enough — the
// same rule the listener registration fact and the event parameter fact run
// under, and the same one `mutation?.()` proved cannot be relaxed.
//
// Report-only for now. Nothing consumes it to decide a verdict; it exists so
// the payoff of writing that contract can be measured before it is written.
import { bindingInScope } from "../../src/ui/roots.ts";
import { flat, isCall, productFiles, trees, walkStack } from "./source.mjs";
import { ownerOf } from "./symbols.mjs";

const SCHEDULERS = new Set(["requestAnimationFrame"]);
const GLOBAL_RECEIVER = /^(window|globalThis|self)$/;
const FN = /^(ArrowFunctionExpression|FunctionExpression)$/;

const facts = [];
for (const [path, tree] of trees) {
  if (!productFiles.has(path)) continue;
  walkStack(tree, (n, stack) => {
    if (!isCall(n)) return;
    const callee = n.callee;
    const bare = callee?.type === "Identifier" ? callee.name : null;
    const member = callee?.type?.endsWith("MemberExpression") && !callee.computed
      ? { obj: callee.object, name: callee.property?.name } : null;
    const name = bare ?? member?.name ?? null;
    if (!name || !SCHEDULERS.has(name)) return;
    const arg = (n.arguments ?? [])[0] ?? null;
    const shape = !arg ? "no-callback" : FN.test(arg.type) ? "function-literal"
      : arg.type === "Identifier" ? "identifier" : "other";
    let eligibility;
    if (bare) {
      eligibility = bindingInScope(bare, stack) ? "shadowed" : "bare-global";
    } else if (member.obj?.type === "Identifier" && GLOBAL_RECEIVER.test(member.obj.name)) {
      eligibility = bindingInScope(member.obj.name, stack) ? "shadowed" : "receiver-proven-global";
    } else {
      eligibility = "unproven-receiver";
    }
    facts.push({ node: n, path, comp: ownerOf(stack), line: n.loc.start.line, name,
      callee: flat(callee), eligibility, shape, arg,
      eligible: eligibility === "bare-global" || eligibility === "receiver-proven-global",
      // Today: a function literal reaches the walk as an unresolved callee, and
      // an identifier reaches it as nothing at all — the callee's name is on
      // the platform list, so the whole call is skipped and the callback with
      // it. The second is a silent drop of the first kind, not a smaller one.
      visibleToday: shape === "function-literal" });
  });
}

const byNode = new Map(facts.filter((f) => f.eligible).map((f) => [f.node, f]));

export { SCHEDULERS, byNode, facts };
