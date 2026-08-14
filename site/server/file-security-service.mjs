import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { inspectZipEntries } from "./zip-reader.mjs";

const execFileAsync = promisify(execFile);
const EICAR_PARTS = ["X5O!P%@AP", "[4\\PZX54(P^)7CC)7}$", "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"];
const ACTIVE_ENTRY = /(?:^|\/)[^/]+\.(?:exe|dll|scr|com|msi|js|jse|vbs|vbe|ps1|bat|cmd|lnk)$/i;
const MACRO_ENTRY = /(?:^|\/)(?:vbaProject\.bin|_VBA_PROJECT|macros?\/)/i;

function scannerUnavailable(message = "External malware scanner is unavailable") {
  const error = new Error(message);
  error.code = "SCANNER_UNAVAILABLE";
  return error;
}

export function createClamAvScanner(executablePath) {
  const command = String(executablePath || "").trim();
  if (!command) return null;
  return {
    name: "ClamAV",
    async scan(file) {
      const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-av-"));
      const target = path.join(directory, "upload.bin");
      try {
        await writeFile(target, file.bytes, { mode: 0o600 });
        try {
          await execFileAsync(command, ["--no-summary", target], { timeout: 60_000, windowsHide: true, maxBuffer: 256 * 1024 });
          return { clean: true };
        } catch (error) {
          if (Number(error?.code) === 1) return { clean: false, threat: "ClamAV detection" };
          throw scannerUnavailable("Configured ClamAV scanner could not complete");
        }
      } finally {
        await rm(directory, { recursive: true, force: true });
      }
    },
  };
}

export function createFileSecurityService(options = {}) {
  const requireExternal = options.requireExternal ?? process.env.INTELIFAR_REQUIRE_EXTERNAL_AV === "true";
  const externalScanner = options.externalScanner ?? createClamAvScanner(options.clamAvPath ?? process.env.INTELIFAR_CLAMSCAN_PATH);
  const maxArchiveEntries = Math.max(1, Number(options.maxArchiveEntries ?? 10_000));
  const maxArchiveBytes = Math.max(1, Number(options.maxArchiveBytes ?? 250 * 1024 * 1024));
  const maxCompressionRatio = Math.max(1, Number(options.maxCompressionRatio ?? 1_000));
  let lastScan = null;

  function status() {
    return {
      mode: externalScanner ? "external-av" : "built-in-preflight",
      engine: externalScanner?.name ?? "intelifar deterministic preflight",
      externalConfigured: Boolean(externalScanner),
      externalRequired: Boolean(requireExternal),
      lastDecision: lastScan?.decision ?? null,
      lastScannedAt: lastScan?.scannedAt ?? null,
    };
  }

  async function scan(file) {
    const bytes = Buffer.isBuffer(file?.bytes) ? file.bytes : Buffer.from(file?.bytes ?? []);
    const sha256 = createHash("sha256").update(bytes).digest("hex");
    const findings = [];
    if (bytes.includes(Buffer.from(EICAR_PARTS.join("")))) findings.push("TEST_MALWARE_SIGNATURE");
    if (bytes.length >= 2 && bytes[0] === 0x4d && bytes[1] === 0x5a) findings.push("DISGUISED_EXECUTABLE");

    if (bytes.length >= 4 && bytes.readUInt32LE(0) === 0x04034b50) {
      try {
        const entries = inspectZipEntries(bytes, { maxEntries: maxArchiveEntries });
        let totalUncompressed = 0;
        for (const entry of entries) {
          totalUncompressed += entry.uncompressedSize;
          if (MACRO_ENTRY.test(entry.name)) findings.push("OFFICE_MACRO");
          if (ACTIVE_ENTRY.test(entry.name)) findings.push("ARCHIVE_ACTIVE_CONTENT");
          const ratio = entry.compressedSize === 0 ? (entry.uncompressedSize > 0 ? Infinity : 1) : entry.uncompressedSize / entry.compressedSize;
          if (entry.uncompressedSize > maxArchiveBytes || ratio > maxCompressionRatio) findings.push("ARCHIVE_BOMB_RISK");
        }
        if (totalUncompressed > maxArchiveBytes) findings.push("ARCHIVE_BOMB_RISK");
      } catch (error) {
        findings.push(error?.code === "ZIP_ENTRY_LIMIT" ? "ARCHIVE_ENTRY_LIMIT" : "MALFORMED_ARCHIVE");
      }
    }

    let level = "preflight";
    if (findings.length === 0) {
      if (!externalScanner && requireExternal) {
        lastScan = { decision: "unavailable", scannedAt: new Date().toISOString() };
        throw scannerUnavailable();
      }
      if (externalScanner) {
        level = "external-av";
        let externalResult;
        try {
          externalResult = await externalScanner.scan({ name: String(file?.name || "upload.bin"), bytes });
        } catch (error) {
          lastScan = { decision: "unavailable", scannedAt: new Date().toISOString() };
          if (requireExternal || error?.code === "SCANNER_UNAVAILABLE") throw scannerUnavailable();
        }
        if (externalResult && externalResult.clean === false) findings.push("EXTERNAL_MALWARE_DETECTED");
      }
    }

    const result = {
      decision: findings.length ? "deny" : "allow",
      level,
      engine: externalScanner?.name ?? "intelifar deterministic preflight",
      findings: [...new Set(findings)],
      sha256,
      scannedAt: new Date().toISOString(),
    };
    lastScan = { decision: result.decision, scannedAt: result.scannedAt };
    return result;
  }

  return { scan, status };
}
