#!/usr/bin/env node
import { build } from "esbuild";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const dist = resolve(root, "dist");
mkdirSync(dist, { recursive: true });

const common = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node22",
  external: ["electron"],
  sourcemap: true,
  logLevel: "info",
};

await build({ ...common, entryPoints: [resolve(root, "src/main/index.ts")], outfile: resolve(dist, "main.cjs") });
await build({ ...common, entryPoints: [resolve(root, "src/preload/index.ts")], outfile: resolve(dist, "preload.cjs") });
// The website-view preload: sandboxed, so it bundles nothing but the channel name.
await build({ ...common, entryPoints: [resolve(root, "src/main/browser/guestPreload.ts")], outfile: resolve(dist, "guest-preload.cjs") });

// hostrpc.Contract.Canonical: sorted keys, no whitespace, no HTML escaping.
function sortKeys(value) {
  if (Array.isArray(value)) return value.map(sortKeys);
  if (value && typeof value === "object") return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortKeys(value[key])]));
  return value;
}

function contractDigest(contract) {
  return "sha256:" + createHash("sha256").update(JSON.stringify(sortKeys(contract))).digest("hex");
}

const generated = resolve(root, "../frontend/src/generated");
const contractSource = resolve(generated, "desktopContract.generated.json");
const contractTarget = resolve(dist, "desktopContract.json");
if (existsSync(contractSource)) {
  const contract = JSON.parse(readFileSync(contractSource, "utf8"));
  const digest = contractDigest(contract);
  const tsSource = resolve(generated, "desktopContract.generated.ts");
  const emitted = existsSync(tsSource) ? /DESKTOP_CONTRACT_DIGEST = "([^"]+)"/.exec(readFileSync(tsSource, "utf8"))?.[1] : undefined;
  if (emitted && emitted !== digest) {
    console.error(`contract digest mismatch: computed ${digest}, generator emitted ${emitted}\nre-run: cd desktop && go run . -emit-contract frontend/src/generated`);
    process.exit(1);
  }
  writeFileSync(contractTarget, JSON.stringify({ ...contract, digest }) + "\n");
  console.log(`embedded contract ${digest} (${contract.commands.length} commands) -> ${contractTarget}`);
} else if (process.env.REASONIX_ELECTRON_ALLOW_MISSING_CONTRACT === "1") {
  rmSync(contractTarget, { force: true });
  console.warn(`contract missing at ${contractSource}; continuing without one (REASONIX_ELECTRON_ALLOW_MISSING_CONTRACT=1)`);
} else {
  console.error(`contract missing at ${contractSource}\nrun: cd desktop && go run . -emit-contract frontend/src/generated`);
  process.exit(1);
}
