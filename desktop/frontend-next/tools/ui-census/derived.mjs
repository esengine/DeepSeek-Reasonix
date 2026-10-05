// Derivations the reports share. Nothing here decides a verdict.
import { sinks } from "./sinks.mjs";
import { productFiles } from "./source.mjs";
import { sites } from "./roots.mjs";

const reachable = sinks.filter((s) => productFiles.has(s.path));
const offstage = sinks.filter((s) => !productFiles.has(s.path));
const unresolvedSinks = sinks.filter((s) => s.unresolved);
const byVerdict = (v) => sites.filter((s) => s.verdict === v);
const unnamedMutations = byVerdict("MUTATION").filter((s) => !s.named);

export { byVerdict, offstage, reachable, unnamedMutations, unresolvedSinks };
