import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (relativePath) =>
  fs.readFileSync(path.join(repoRoot, relativePath), "utf8");

const frontendPackage = JSON.parse(read("desktop/frontend/package.json"));
const wailsConfig = JSON.parse(read("desktop/wails.json"));
const ciWorkflow = read(".github/workflows/ci.yml");
const releaseWorkflow = read(".github/workflows/release-desktop.yml");
const readme = read("README.md");
const desktopReadme = read("desktop/README.md");
const desktopBuildScript = read("scripts/desktop-build.sh");

const jobBody = (workflow, jobName) => {
  const lines = workflow.split("\n");
  const start = lines.findIndex((line) => line === `  ${jobName}:`);
  assert.notEqual(start, -1, `workflow must define the ${jobName} job`);
  const nextJob = lines
    .slice(start + 1)
    .findIndex((line) => /^  [a-zA-Z0-9_-]+:$/.test(line));
  const end = nextJob === -1 ? lines.length : start + 1 + nextJob;
  return lines.slice(start, end).join("\n");
};

const nodeVersions = (workflow) =>
  [...workflow.matchAll(/node-version:\s*["']?(\d+)/g)].map(
    (match) => match[1],
  );

assert.equal(read("desktop/frontend/.nvmrc").trim(), "24");
assert.equal(frontendPackage.engines?.node, ">=24");
assert.equal(frontendPackage.engines?.pnpm, ">=10 <11");
assert.equal(
  wailsConfig["frontend:install"],
  "pnpm install --config.confirmModulesPurge=false",
);

for (const jobName of ["desktop", "desktop-windows"]) {
  assert.deepEqual(nodeVersions(jobBody(ciWorkflow, jobName)), ["24"]);
}

const releaseNodeVersions = nodeVersions(releaseWorkflow);
assert.ok(releaseNodeVersions.length > 0, "release workflow must set up Node");
assert.deepEqual(new Set(releaseNodeVersions), new Set(["24"]));

for (const [name, workflow] of [
  ["CI", ciWorkflow],
  ["release", releaseWorkflow],
]) {
  const lines = workflow.split("\n");
  const pnpmVersions = lines.flatMap((line, index) => {
    if (!line.includes("pnpm/action-setup@")) return [];
    const block = lines.slice(index, index + 5).join("\n");
    return [block.match(/version:\s*(\d+)/)?.[1] ?? "missing"];
  });
  assert.ok(pnpmVersions.length > 0, `${name} workflow must set up pnpm`);
  assert.deepEqual(new Set(pnpmVersions), new Set(["10"]));
}

for (const [name, content] of [
  ["README.md", readme],
  ["desktop/README.md", desktopReadme],
]) {
  assert.match(content, /npm i(?:nstall)? -g pnpm@10/);
  assert.match(content, /make wails-install/);
  assert.doesNotMatch(
    content,
    /github\.com\/wailsapp\/wails\/v2\/cmd\/wails@/,
    `${name} must use the shared Wails installer`,
  );
}

assert.match(readme, /#### CLI/);
assert.match(readme, /#### Desktop/);
assert.match(desktopBuildScript, /make wails-install/);
assert.doesNotMatch(
  desktopBuildScript,
  /github\.com\/wailsapp\/wails\/v2\/cmd\/wails@/,
);

console.log("desktop build contract: PASS");
