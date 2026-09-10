import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (relativePath) =>
  fs.readFileSync(path.join(repoRoot, relativePath), "utf8");

const frontendPackage = JSON.parse(read("desktop/frontend/package.json"));
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
assert.ok(
  !fs.existsSync(path.join(repoRoot, "desktop/wails.json")),
  "desktop/wails.json must be retired with the Wails shell",
);

for (const jobName of ["desktop-prepare", "desktop-go", "desktop-frontend", "desktop-browser", "desktop-macos", "desktop-windows"]) {
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
  assert.doesNotMatch(
    content,
    /wails/i,
    `${name} must not reference the retired Wails toolchain`,
  );
}

assert.match(readme, /#### CLI/);
assert.match(readme, /#### Desktop/);

// The desktop build is the Electron packaging entrypoint: it must regenerate
// the shell/service contract and fail on drift before compiling anything.
assert.match(
  desktopBuildScript,
  /go run \. -emit-contract frontend\/src\/generated/,
  "desktop builds must regenerate the host contract",
);
assert.match(
  desktopBuildScript,
  /git -C "\$ROOT" diff --exit-code -- desktop\/frontend\/src\/generated/,
  "desktop builds must fail on host contract drift",
);
// The release channel now rides in the Go service ldflags (the shell reads
// the same identity from resources/build.json written by package.mjs).
assert.match(
  desktopBuildScript,
  /service_ldflags="-X main\.version=\$VERSION -X main\.channel=\$CHANNEL/,
  "desktop builds must link the release channel into the Go service",
);
// The shell is packaged through the Electron packaging script, never wails build.
assert.match(
  desktopBuildScript,
  /node "\$ROOT\/desktop\/packaging\/package\.mjs" "\$PLATFORM" "\$VERSION" "\$CHANNEL"/,
  "desktop builds must package the shell through desktop/packaging/package.mjs",
);
assert.doesNotMatch(desktopBuildScript, /wails build/);
assert.doesNotMatch(
  desktopBuildScript,
  /github\.com\/wailsapp\/wails\/v2\/cmd\/wails@/,
);
// darwin/universal still ships one fat binary per Go artifact.
assert.match(
  desktopBuildScript,
  /lipo -create "\$service_tmp\/amd64" "\$service_tmp\/arm64" -output "\$service_out"/,
  "darwin universal builds must lipo the desktop service",
);
// Windows keeps one canonical SignPath payload: signing-files.txt enumerates
// every PE file, then package-windows-desktop.sh rebuilds from the payload.
assert.match(
  desktopBuildScript,
  /node "\$ROOT\/desktop\/packaging\/signing-files\.mjs" "\$payload_dir"/,
  "windows builds must enumerate the signing payload",
);
assert.match(
  desktopBuildScript,
  /VERSION="\$VERSION" "\$ROOT\/scripts\/package-windows-desktop\.sh" "\$arch" "\$payload_dir"/,
  "windows builds must package from the signing payload",
);

console.log("desktop build contract: PASS");
