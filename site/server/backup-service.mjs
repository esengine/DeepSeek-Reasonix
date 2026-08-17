import { createHash, randomBytes } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdir, readFile, readdir, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import Database from "better-sqlite3";

const BACKUP_ID = /^BKP-\d{8}T\d{6}-[a-f0-9]{12}$/;
const REQUIRED_TABLES = ["agent_task_events", "agent_tasks", "analysis_jobs", "audit_events", "invitations", "secure_shares", "sessions", "users", "wiki_review_requests", "wiki_versions", "workspaces"];

function makeId(now = new Date()) {
  const stamp = now.toISOString().replaceAll(/[-:]/g, "").slice(0, 15);
  return `BKP-${stamp}-${randomBytes(6).toString("hex")}`;
}

async function sha256File(filePath) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest("hex");
}

function validateId(id) {
  const value = String(id || "");
  if (!BACKUP_ID.test(value)) {
    const error = new Error("Invalid backup identifier");
    error.code = "INVALID_BACKUP_ID";
    throw error;
  }
  return value;
}

function inspectDatabase(filePath) {
  const database = new Database(filePath, { readonly: true, fileMustExist: true });
  try {
    const integrity = database.pragma("integrity_check", { simple: true });
    const tables = new Set(database.prepare("SELECT name FROM sqlite_master WHERE type = 'table'").all().map((row) => row.name));
    return { valid: integrity === "ok" && REQUIRED_TABLES.every((name) => tables.has(name)), integrity, requiredTables: REQUIRED_TABLES.filter((name) => tables.has(name)).length };
  } finally {
    database.close();
  }
}

export function createBackupService(options) {
  if (!options?.store?.backupTo) throw new Error("A SQLite platform store is required for backups");
  const store = options.store;
  const backupRoot = path.resolve(options.backupRoot ?? process.env.INTELIFAR_BACKUP_ROOT ?? path.resolve(process.cwd(), ".runtime", "backups"));
  const retention = Math.max(1, Math.min(90, Number(options.retention ?? process.env.INTELIFAR_BACKUP_RETENTION ?? 7)));
  let backupRunning = false;

  function pathsFor(id) {
    const safeId = validateId(id);
    return {
      database: path.join(backupRoot, `${safeId}.sqlite`),
      manifest: path.join(backupRoot, `${safeId}.manifest.json`),
    };
  }

  async function writeManifest(manifest) {
    const { manifest: target } = pathsFor(manifest.id);
    const temporary = `${target}.partial`;
    await writeFile(temporary, `${JSON.stringify(manifest, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
    await rename(temporary, target);
  }

  async function listBackups() {
    await mkdir(backupRoot, { recursive: true });
    const names = await readdir(backupRoot);
    const manifests = [];
    for (const name of names.filter((item) => item.endsWith(".manifest.json"))) {
      const id = name.slice(0, -".manifest.json".length);
      if (!BACKUP_ID.test(id)) continue;
      try {
        const manifest = JSON.parse(await readFile(path.join(backupRoot, name), "utf8"));
        if (manifest.id === id) manifests.push(manifest);
      } catch {
        // Ignore incomplete or manually corrupted manifests; verification will surface missing backups.
      }
    }
    return manifests.sort((a, b) => String(b.createdAt).localeCompare(String(a.createdAt)));
  }

  async function prune() {
    const items = await listBackups();
    for (const item of items.slice(retention)) {
      const paths = pathsFor(item.id);
      await Promise.all([rm(paths.database, { force: true }), rm(paths.manifest, { force: true })]);
    }
  }

  async function createBackup(input = {}) {
    if (backupRunning) {
      const error = new Error("A verified backup is already in progress");
      error.code = "BACKUP_IN_PROGRESS";
      throw error;
    }
    backupRunning = true;
    let temporary = null;
    try {
      await mkdir(backupRoot, { recursive: true });
      const id = makeId();
      const paths = pathsFor(id);
      temporary = `${paths.database}.partial`;
      await store.backupTo(temporary);
      const inspection = inspectDatabase(temporary);
      if (!inspection.valid) {
        const error = new Error("SQLite backup integrity verification failed");
        error.code = "BACKUP_INTEGRITY_FAILED";
        throw error;
      }
      await rename(temporary, paths.database);
      const fileStat = await stat(paths.database);
      const manifest = {
        id,
        createdAt: new Date().toISOString(),
        createdBy: input.createdBy ? String(input.createdBy) : null,
        size: fileStat.size,
        sha256: await sha256File(paths.database),
        integrity: "ok",
        requiredTables: inspection.requiredTables,
      };
      await writeManifest(manifest);
      await prune();
      return manifest;
    } catch (error) {
      if (temporary) await rm(temporary, { force: true });
      throw error;
    } finally {
      backupRunning = false;
    }
  }

  async function verifyBackup(id) {
    const paths = pathsFor(id);
    const manifest = JSON.parse(await readFile(paths.manifest, "utf8"));
    const inspection = inspectDatabase(paths.database);
    const sha256 = await sha256File(paths.database);
    const valid = inspection.valid && sha256 === manifest.sha256;
    const updated = { ...manifest, integrity: valid ? "ok" : "failed", verifiedAt: new Date().toISOString(), currentSha256: sha256 };
    await writeManifest(updated);
    if (!valid) {
      const error = new Error("Backup verification failed");
      error.code = "BACKUP_INTEGRITY_FAILED";
      throw error;
    }
    return updated;
  }

  return { createBackup, listBackups, verifyBackup, root: backupRoot };
}
