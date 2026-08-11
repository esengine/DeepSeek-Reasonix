// Copies the chrome renderer assets into dist/. The TypeScript compiler only
// emits JS; chrome.html and chrome.css are plain assets the main process
// loads via loadFile, so they must be copied explicitly. Fails when any
// source asset is missing so a broken build cannot ship silently.
import { copyFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const outDir = join(root, "dist");

for (const name of ["chrome.html", "chrome.css"]) {
  const src = join(root, "src", name);
  if (!existsSync(src)) {
    console.error(`missing chrome asset: ${src}`);
    process.exit(1);
  }
  mkdirSync(outDir, { recursive: true });
  copyFileSync(src, join(outDir, name));
  console.log(`copied ${name}`);
}
