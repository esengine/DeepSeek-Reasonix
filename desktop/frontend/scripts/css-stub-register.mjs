// Registers the css-loader hook before tsx boots, so `.css` imports in
// component modules resolve to an empty module instead of failing on the
// extension. Mirrors svg-loader.mjs; used by css-stub-register.mjs.
import { register } from "node:module";
register(new URL("./css-loader.mjs", import.meta.url));
