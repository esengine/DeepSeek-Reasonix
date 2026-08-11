// Registers the svg-loader hook before tsx boots, so asset imports in
// component tests resolve to a stub instead of failing on the extension.
import { register } from "node:module";
register(new URL("./svg-loader.mjs", import.meta.url));
