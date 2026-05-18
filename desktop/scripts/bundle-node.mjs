// Pulls Node 22 from nodejs.org into desktop/src-tauri/binaries/.
// Mac always produces a universal Mach-O so Intel + Apple Silicon ship from one .dmg (issue #1234).
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

if (existsSync(targetExe) && statSync(targetExe).size > 1024 * 1024) {
  if (PLAT === "darwin") {
    try {
      const archs = execSync(`lipo -archs "${targetExe}"`, { encoding: "utf8" }).trim();
      if (archs.includes("arm64") && archs.includes("x86_64")) {
        console.log(`${targetExe} already universal (${archs}) — delete to refetch`);
        process.exit(0);
      }
      console.log(`${targetExe} present but not universal (${archs}) — rebuilding`);
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

if (PLAT === "darwin") {
  const [arm, x64] = await Promise.all([fetchAndExtract("arm64"), fetchAndExtract("x64")]);

  console.log("Creating universal binary with lipo ...");
  if (existsSync(targetExe)) rmSync(targetExe);
  execSync(`lipo -create "${arm.inner}" "${x64.inner}" -output "${targetExe}"`, { stdio: "inherit" });
  chmodSync(targetExe, 0o755);

  rmSync(arm.extractDir, { recursive: true, force: true });
  rmSync(x64.extractDir, { recursive: true, force: true });

  const archs = execSync(`lipo -archs "${targetExe}"`, { encoding: "utf8" }).trim();
  const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
  console.log(`Done: ${targetExe} (${mb} MB, archs: ${archs})`);
} else {
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

  const mb = (statSync(targetExe).size / 1024 / 1024).toFixed(1);
  console.log(`Done: ${targetExe} (${mb} MB)`);
}
