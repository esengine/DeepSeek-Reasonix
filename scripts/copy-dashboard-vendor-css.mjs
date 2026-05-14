import { copyFileSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

const targets = [
  ["node_modules/highlight.js/styles/github-dark.min.css", "dashboard/dist/vendor-hljs.css"],
  ["node_modules/uplot/dist/uPlot.min.css", "dashboard/dist/vendor-uplot.css"],
];

for (const [src, dst] of targets) {
  mkdirSync(dirname(resolve(dst)), { recursive: true });
  copyFileSync(resolve(src), resolve(dst));
  console.log(`copied ${src} → ${dst}`);
}

// Marks dist/cli/ as ESM so Node skips the CJS-then-ESM reparse warning when
// the bundle is loaded outside its own npm install (e.g. desktop sidecar).
const cliMarker = resolve("dist/cli/package.json");
mkdirSync(dirname(cliMarker), { recursive: true });
writeFileSync(cliMarker, `${JSON.stringify({ type: "module" }, null, 2)}\n`);
console.log(`wrote ${cliMarker}`);
