// Pulls Node 22 from nodejs.org into desktop/src-tauri/binaries/.
// macOS downloads the host-arch binary so arm64 and x64 releases stay split.
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
const HOST_ARCH = ARCH_RAW === "arm64" ? "arm64" : ARCH_RAW === "x64" ? "x64" : null;

if (!HOST_ARCH || !["win32", "darwin", "linux"].includes(PLAT)) {
  console.error(`Unsupported host: ${PLAT}/${ARCH_RAW}. Supported: win32 / darwin / linux × x64 / arm64.`);
  process.exit(1);
}

const isWin = PLAT === "win32";
const targetExe = join(binDir, isWin ? "node.exe" : "node");
const MAC_ARCH = HOST_ARCH === "x64" ? "x86_64" : HOST_ARCH;

function adhocSignMac(binPath) {
  // #1611: child node needs com.apple.security.inherit so it inherits the
  // parent .app's TCC grants (Documents folder etc.) instead of re-prompting
  // on every spawn. Ad-hoc is enough for TCC; CI later overlays Developer ID.
  const ents = join(here, "..", "src-tauri", "entitlements", "node-helper.plist");
  if (!existsSync(ents)) {
    throw new Error(`entitlements missing: ${ents}`);
  }
  execSync(
    `codesign --force --sign - --options runtime --entitlements "${ents}" "${binPath}"`,
    { stdio: "inherit" },
  );
  if (!hasMacTccInheritance(binPath)) {
    throw new Error(`codesign did not apply com.apple.security.inherit to ${binPath}`);
  }
}

function hasMacTccInheritance(binPath) {
  try {
    const out = execSync(`codesign -d --entitlements :- "${binPath}" 2>&1`, { encoding: "utf8" });
    return out.includes("com.apple.security.inherit") && out.includes("<true/>");
  } catch {
    return false;
  }
}

if (existsSync(targetExe) && statSync(targetExe).size > 1024 * 1024) {
  if (PLAT === "darwin") {
    try {
      const archs = execSync(`lipo -archs "${targetExe}"`, { encoding: "utf8" }).trim();
      if (archs === MAC_ARCH) {
        if (!hasMacTccInheritance(targetExe)) {
          console.log(`${targetExe} ${MAC_ARCH} build missing TCC inheritance — applying ad-hoc sign`);
          adhocSignMac(targetExe);
        } else {
          console.log(`${targetExe} already ${MAC_ARCH} + TCC-inheriting — delete to refetch`);
        }
        process.exit(0);
      }
      console.log(`${targetExe} present but not ${MAC_ARCH} (${archs}) — rebuilding`);
      rmSync(targetExe);
    } catch {
      console.log(`${targetExe} present but not verifiable as Mach-O — rebuilding`);
      rmSync(targetExe);
    }
  } else {
    const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
    console.log(`${targetExe} already present (${mb} MB) — delete to refetch`);
    process.exit(0);
  }
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

async function fetchAndExtract(arch) {
  const triple =
    PLAT === "win32" ? `win-${arch}` : PLAT === "darwin" ? `darwin-${arch}` : `linux-${arch}`;
  const archiveExt = PLAT === "win32" ? "zip" : PLAT === "darwin" ? "tar.gz" : "tar.xz";
  const archiveBase = `node-v${NODE_VERSION}-${triple}`;
  const archiveFile = `${archiveBase}.${archiveExt}`;
  const url = `https://nodejs.org/dist/v${NODE_VERSION}/${archiveFile}`;
  const archivePath = join(binDir, archiveFile);
  const extractDir = join(binDir, `_extract_${arch}`);

  console.log(`Downloading ${archiveFile} ...`);
  await follow(url, archivePath);
  process.stdout.write("\n");

  rmSync(extractDir, { recursive: true, force: true });
  mkdirSync(extractDir, { recursive: true });

  console.log(`Extracting ${arch} ...`);
  if (isWin) {
    execSync(
      `powershell -NoProfile -Command "Expand-Archive -Force -Path '${archivePath}' -DestinationPath '${extractDir}'"`,
      { stdio: "inherit" },
    );
  } else {
    execSync(`tar -xf "${archivePath}" -C "${extractDir}"`, { stdio: "inherit" });
  }

  const inner = isWin
    ? join(extractDir, archiveBase, "node.exe")
    : join(extractDir, archiveBase, "bin", "node");
  if (!existsSync(inner)) {
    console.error(`Extracted binary not found at expected path: ${inner}`);
    process.exit(1);
  }

  rmSync(archivePath);
  return { inner, extractDir };
}

const { inner, extractDir } = await fetchAndExtract(HOST_ARCH);

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

if (PLAT === "darwin") {
  adhocSignMac(targetExe);
  const archs = execSync(`lipo -archs "${targetExe}"`, { encoding: "utf8" }).trim();
  const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
  console.log(`Done: ${targetExe} (${mb} MB, archs: ${archs})`);
} else {
  const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
  console.log(`Done: ${targetExe} (${mb} MB)`);
}
