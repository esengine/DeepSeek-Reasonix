// The event-registration facts, certified once by the module the repository's
// own gate uses. Three readers consume a projection of them and none re-derives
// one: the root census takes ADD with a user-input event as an entry point, the
// effect walk takes ADD and REMOVE as install and uninstall, and the classifier
// takes a proven fact as a call whose callee is the platform's and whose
// listener this execution does not run.
//
// Keyed by node identity, because the fact and its consumers walk the same
// tree. A second lookup by name would be a second authority.
import { eventRegistrations } from "../../src/ui/roots.ts";
import { productFiles, resolveSpec, trees } from "./source.mjs";

const facts = eventRegistrations(new Map([...trees].filter(([p]) => productFiles.has(p))), {
  resolve: (from, spec) => resolveSpec(from, spec),
});

// Only a fact with nothing missing may excuse a call. A shadowed callee, a
// wrapper and an unproven receiver stay ordinary calls and keep whatever the
// walk would otherwise have said about them.
const byNode = new Map();
for (const f of facts) if (!f.refusal) byNode.set(f.node, f);

export { byNode, facts };
