#!/usr/bin/env node
// Enumerates every PE file in a Windows signing payload (the flat Go
// executables plus the Electron app/ tree) into signing-files.txt, the list
// the SignPath contract and the Authenticode verifier consume.
//
// usage: node desktop/packaging/signing-files.mjs <payload-dir> [--check]
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { parseSigningFileList, PRODUCT, signingFileList, walkFiles, WINDOWS_FLAT_PAYLOAD } from "./lib.mjs";

const [dirArg, mode] = process.argv.slice(2);
if (!dirArg || (mode && mode !== "--check")) {
  console.error("usage: signing-files.mjs <payload-dir> [--check]");
  process.exit(2);
}
const dir = resolve(dirArg);
const listPath = join(dir, "signing-files.txt");
const files = signingFileList(walkFiles(dir));
const problems = [];
for (const name of WINDOWS_FLAT_PAYLOAD) if (!files.includes(name)) problems.push(`flat payload executable is missing: ${name}`);
if (!files.includes(`app/${PRODUCT.executable}.exe`)) problems.push(`Electron shell is missing: app/${PRODUCT.executable}.exe`);
if (problems.length > 0) {
  for (const problem of problems) console.error(`signing-files: ${problem}`);
  process.exit(1);
}

if (mode === "--check") {
  if (!existsSync(listPath)) {
    console.error(`signing-files: ${listPath} is missing`);
    process.exit(1);
  }
  const recorded = parseSigningFileList(readFileSync(listPath, "utf8"));
  const missing = recorded.filter((name) => !files.includes(name));
  const extra = files.filter((name) => !recorded.includes(name));
  if (missing.length > 0 || extra.length > 0) {
    for (const name of missing) console.error(`signing-files: listed but absent: ${name}`);
    for (const name of extra) console.error(`signing-files: present but unlisted: ${name}`);
    process.exit(1);
  }
  console.log(`signing-files: ${files.length} PE files match ${listPath}`);
} else {
  writeFileSync(listPath, files.join("\n") + "\n");
  console.log(`signing-files: wrote ${files.length} PE files to ${listPath}`);
}
