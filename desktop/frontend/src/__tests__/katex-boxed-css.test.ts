import { readFileSync } from "node:fs";

const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const packageJSON = JSON.parse(readFileSync(new URL("../../package.json", import.meta.url), "utf8"));

const boxedRule = styles.match(/\.md\s+\.katex\s+\.fbox\s*\{([^}]*)\}/s)?.[1] ?? "";
if (!/width:\s*100%\s*;/.test(boxedRule)) {
  throw new Error("KaTeX boxed frame must keep a standards-compatible width fallback");
}
if (!/width:\s*-webkit-fill-available\s*;/.test(boxedRule)) {
  throw new Error("KaTeX boxed frame must fill the containing row in WebKit");
}
if (packageJSON.packageManager !== "pnpm@10.34.5") {
  throw new Error(`frontend package manager pin drifted: ${packageJSON.packageManager ?? "missing"}`);
}

console.log("katex boxed CSS/package contract: PASS");
