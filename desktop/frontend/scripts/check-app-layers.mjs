#!/usr/bin/env node

import { readdirSync, readFileSync } from "node:fs";
import { basename, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const sourceRoot = fileURLToPath(new URL("../src", import.meta.url));
const shellRoot = join(sourceRoot, "app-shell");
const runtimeRoot = join(sourceRoot, "app-runtime");
const domainOwners = new Set(["operationOwner.ts", "navigationOwner.ts", "sessionActionOwner.ts", "sessionTarget.ts"]);
const importPattern = /(?:import|export)\s+(?:type\s+)?(?:[^"']+?\s+from\s+)?["']([^"']+)["']/g;

function sourceFiles(root) {
  return readdirSync(root, { withFileTypes: true })
    .flatMap((entry) => entry.isDirectory() ? sourceFiles(join(root, entry.name)) : [join(root, entry.name)])
    .filter((file) => /\.(?:ts|tsx)$/.test(file));
}

const failures = [];
for (const file of [...sourceFiles(shellRoot), ...sourceFiles(runtimeRoot)]) {
  const code = readFileSync(file, "utf8");
  const rel = relative(sourceRoot, file).replaceAll("\\", "/");
  const imports = Array.from(code.matchAll(importPattern), (match) => match[1]);
  if (rel.startsWith("app-shell/") && imports.some((value) => value.includes("lib/bridge"))) {
    failures.push(`${rel}: presentation regions must receive runtime commands through props, not import the Wails bridge`);
  }
  if (domainOwners.has(basename(file))) {
    if (imports.some((value) => value === "react" || value.startsWith("react/") || value.includes("app-shell/") || value.includes("components/"))) {
      failures.push(`${rel}: domain owners must not depend on React, DOM presentation, or shell regions`);
    }
    if (/\b(?:window|document|HTMLElement|ReactNode|SyntheticEvent)\b/.test(code)) {
      failures.push(`${rel}: domain owners must operate on explicit values rather than DOM or React objects`);
    }
  }
  if (rel.startsWith("app-runtime/") && imports.some((value) => value.includes("app-shell/"))) {
    failures.push(`${rel}: runtime/domain layers must not import the presentation layer`);
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`check-app-layers: ${failure}`);
  process.exit(1);
}
console.log("check-app-layers: App dependency direction is valid");
