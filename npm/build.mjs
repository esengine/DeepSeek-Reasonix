import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..");
const STAGE = join(HERE, ".stage");

const TARGETS = [
  { node: "darwin-arm64", goos: "darwin", goarch: "arm64" },
  { node: "darwin-x64", goos: "darwin", goarch: "amd64" },
  { node: "linux-arm64", goos: "linux", goarch: "arm64" },
  { node: "linux-x64", goos: "linux", goarch: "amd64" },
  { node: "win32-arm64", goos: "windows", goarch: "arm64" },
  { node: "win32-x64", goos: "windows", goarch: "amd64" },
];

const tag = process.argv[2] ?? process.env.GITHUB_REF_NAME;
if (!tag) {
  console.error("usage: node npm/build.mjs <tag>   (e.g. v1.0.0 or npm-v1.0.0)");
  process.exit(1);
}
// npm ships on its own `npm-v*` tag (release-npm.yml); also accept a bare `v*`.
const version = tag.replace(/^(npm-)?v/, "");
const publish = process.argv.includes("--publish");

rmSync(STAGE, { recursive: true, force: true });
mkdirSync(STAGE, { recursive: true });

const subPackages = [];
for (const t of TARGETS) {
  const name = `@reasonix/cli-${t.node}`;
  const dir = join(STAGE, `cli-${t.node}`);
  const exe = t.goos === "windows" ? "reasonix.exe" : "reasonix";
  mkdirSync(join(dir, "bin"), { recursive: true });

  console.log(`build ${t.goos}/${t.goarch} -> ${name}`);
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      `-s -w -X main.version=${tag}`,
      "-o",
      join(dir, "bin", exe),
      "./cmd/reasonix",
    ],
    {
      cwd: ROOT,
      stdio: "inherit",
      env: { ...process.env, CGO_ENABLED: "0", GOOS: t.goos, GOARCH: t.goarch },
    },
  );

  writeFileSync(
    join(dir, "package.json"),
    `${JSON.stringify(
      {
        name,
        version,
        description: `reasonix prebuilt binary for ${t.node}.`,
        os: [t.goos === "windows" ? "win32" : t.goos],
        cpu: [t.goarch === "amd64" ? "x64" : "arm64"],
        files: ["bin/"],
        license: "MIT",
        repository: {
          type: "git",
          url: "git+https://github.com/esengine/DeepSeek-Reasonix.git",
        },
      },
      null,
      2,
    )}\n`,
  );
  subPackages.push({ name, dir });
}

const mainDir = join(STAGE, "reasonix");
mkdirSync(mainDir, { recursive: true });
cpSync(join(HERE, "reasonix", "bin"), join(mainDir, "bin"), { recursive: true });
cpSync(join(ROOT, "README.md"), join(mainDir, "README.md"));

const mainPkg = JSON.parse(
  readFileSync(join(HERE, "reasonix", "package.json"), "utf8"),
);
mainPkg.version = version;
for (const key of Object.keys(mainPkg.optionalDependencies)) {
  mainPkg.optionalDependencies[key] = version;
}
writeFileSync(
  join(mainDir, "package.json"),
  `${JSON.stringify(mainPkg, null, 2)}\n`,
);

if (!publish) {
  console.log(`\nstaged ${version} in ${STAGE} (dry run; pass --publish to publish)`);
  process.exit(0);
}

// Three independent dist-tags: 0.x stable is the promoted default (`latest`); a
// `-canary.` build is the opt-in tester channel (`canary`); everything else — the
// 1.x line and rc prereleases — ships under `next`. Only a `--tag canary` publish
// moves canary, so `next`/`latest` users never resolve a canary. Promote a 1.x
// stable to default with a manual `npm dist-tag add reasonix@<ver> latest`.
const distTag = version.includes("-canary.")
  ? "canary"
  : version.startsWith("0.") && !version.includes("-")
    ? "latest"
    : "next";
const publishArgs = ["publish", "--access", "public", "--tag", distTag];

for (const sub of subPackages) {
  console.log(`publish ${sub.name}@${version} (${distTag})`);
  execFileSync("npm", publishArgs, { cwd: sub.dir, stdio: "inherit" });
}
console.log(`publish reasonix@${version} (${distTag})`);
execFileSync("npm", publishArgs, { cwd: mainDir, stdio: "inherit" });
