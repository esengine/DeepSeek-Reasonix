// The certified interaction universe, produced once by the module the
// repository's own gate uses. Separated from roots.mjs so that what a root
// reaches — which is the classifier's answer — can be built on facts the
// certification already knows, without the classifier and the certification
// depending on each other.
import { certifyRoots } from "../../src/ui/roots.ts";
import { productFiles, resolveSpec, trees } from "./source.mjs";
import { PRIMITIVES } from "./sites.mjs";

const census = certifyRoots(new Map([...trees].filter(([p]) => productFiles.has(p))), {
  skip: (p) => PRIMITIVES.has(p),
  resolve: (from, spec) => resolveSpec(from, spec),
});

export { census };
