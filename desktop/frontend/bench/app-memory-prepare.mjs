import { cpSync, mkdirSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildIdentity } from "./app-memory-evidence.mjs";
import { MEMORY_PROTOCOL, verifyIdentity } from "./app-memory-shards.mjs";
const frontend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const target = process.argv[2];
const executionId = `${process.env.GITHUB_RUN_ID}:${process.env.GITHUB_RUN_ATTEMPT}`;
if (!target || !process.env.GITHUB_RUN_ID || !process.env.GITHUB_RUN_ATTEMPT) throw new Error("prepare requires an output directory and workflow identity");
// Vite removes this tracked placeholder; restore it before hashing the clean build.
const prefix = execFileSync("git", ["rev-parse", "--show-prefix"], { cwd: frontend, encoding: "utf8" }).trim();
writeFileSync(path.join(frontend, "dist/.gitkeep"), execFileSync("git", ["show", `HEAD:${prefix}dist/.gitkeep`], { cwd: frontend }));
const identity = buildIdentity(frontend);
verifyIdentity(identity, identity);
if (identity.sourceSHA !== process.env.EXPECTED_SOURCE_SHA) throw new Error("prepared build is not the requested commit");
mkdirSync(target, { recursive: true });
cpSync(path.join(frontend, "dist"), path.join(target, "dist"), { recursive: true });
writeFileSync(path.join(target, "identity.json"), JSON.stringify({ identity, executionId, protocol: MEMORY_PROTOCOL }, null, 2));
