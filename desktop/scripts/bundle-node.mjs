/**
 * Cross-platform Node 22 downloader. Detects host platform + arch and pulls
 * the matching official Node distribution from nodejs.org, then extracts the
 * node binary into desktop/src-tauri/binaries/. Tauri's per-platform conf
 * overlay (tauri.<platform>.conf.json) picks it up at bundle time.
 *
 * Usage: `npm run bundle:node` from desktop/.
 * Re-run when bumping NODE_VERSION. The download is gitignored.
 */
import { execSync } from "node:child_process";
import { chmodSync, createWriteStream, existsSync, mkdirSync, renameSync, rmSync, statSync } from "node:fs";
import https from "node:https";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const NODE_VERSION = "22.13.0";

const here = dirname(fileURLToPath(import.meta.url));
const binDir = join(here, "..", "src-tauri", "binaries");

const PLAT = process.platform;
const ARCH_RAW = process.arch;
const ARCH = ARCH_RAW === "arm64" ? "arm64" : ARCH_RAW === "x64" ? "x64" : null;

if (!ARCH || !["win32", "darwin", "linux"].includes(PLAT)) {
  console.error(`Unsupported host: ${PLAT}/${ARCH_RAW}. Supported: win32 / darwin / linux × x64 / arm64.`);
  process.exit(1);
}

const isWin = PLAT === "win32";
const triple =
  PLAT === "win32" ? `win-${ARCH}` : PLAT === "darwin" ? `darwin-${ARCH}` : `linux-${ARCH}`;
const archiveExt = PLAT === "win32" ? "zip" : PLAT === "darwin" ? "tar.gz" : "tar.xz";
const archiveBase = `node-v${NODE_VERSION}-${triple}`;
const archiveFile = `${archiveBase}.${archiveExt}`;
const url = `https://nodejs.org/dist/v${NODE_VERSION}/${archiveFile}`;

const targetExe = join(binDir, isWin ? "node.exe" : "node");
const archivePath = join(binDir, archiveFile);
const extractDir = join(binDir, "_extract");

if (existsSync(targetExe)) {
  const size = statSync(targetExe).size;
  if (size > 1024 * 1024) {
    console.log(`${targetExe} already present (${(size / 1024 / 1024).toFixed(1)} MB) — delete to refetch`);
    process.exit(0);
  }
  // < 1 MB = placeholder, refetch.
}

mkdirSync(binDir, { recursive: true });

function follow(url, dest, redirects = 5) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, (res) => {
      const status = res.statusCode ?? 0;
      if ((status === 301 || status === 302 || status === 307 || status === 308) && res.headers.location && redirects > 0) {
        res.resume();
        follow(new URL(res.headers.location, url).toString(), dest, redirects - 1).then(resolve, reject);
        return;
      }
      if (status !== 200) {
        reject(new Error(`HTTP ${status} fetching ${url}`));
        return;
      }
      const file = createWriteStream(dest);
      const total = Number.parseInt(res.headers["content-length"] ?? "0", 10);
      let got = 0;
      let last = 0;
      res.on("data", (chunk) => {
        got += chunk.length;
        if (total && Date.now() - last > 250) {
          process.stdout.write(`\r  ${(got / 1024 / 1024).toFixed(1)} / ${(total / 1024 / 1024).toFixed(1)} MB`);
          last = Date.now();
        }
      });
      res.pipe(file);
      file.on("finish", () => file.close((err) => (err ? reject(err) : resolve())));
      file.on("error", reject);
    });
    req.on("error", reject);
  });
}

console.log(`Downloading ${archiveFile} (Node v${NODE_VERSION} ${triple}) ...`);
await follow(url, archivePath);
process.stdout.write("\n");

rmSync(extractDir, { recursive: true, force: true });
mkdirSync(extractDir, { recursive: true });

console.log("Extracting ...");
if (isWin) {
  execSync(
    `powershell -NoProfile -Command "Expand-Archive -Force -Path '${archivePath}' -DestinationPath '${extractDir}'"`,
    { stdio: "inherit" },
  );
} else {
  // tar handles both .tar.gz (with -z auto) and .tar.xz (needs xz-utils on host).
  execSync(`tar -xf "${archivePath}" -C "${extractDir}"`, { stdio: "inherit" });
}

const inner = isWin
  ? join(extractDir, archiveBase, "node.exe")
  : join(extractDir, archiveBase, "bin", "node");
if (!existsSync(inner)) {
  console.error(`Extracted binary not found at expected path: ${inner}`);
  process.exit(1);
}

if (existsSync(targetExe)) rmSync(targetExe);
renameSync(inner, targetExe);
if (!isWin) {
  try {
    chmodSync(targetExe, 0o755);
  } catch {
    /* ignore */
  }
}
rmSync(extractDir, { recursive: true, force: true });
rmSync(archivePath);

const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
console.log(`Done: ${targetExe} (${mb} MB)`);
