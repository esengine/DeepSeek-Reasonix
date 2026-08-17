import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createBackupService } from "./backup-service.mjs";
import { createPlatformStore } from "./platform-store.mjs";

async function fixture(retention = 7) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-backup-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
  return {
    directory,
    store,
    service: createBackupService({ store, backupRoot: path.join(directory, "backups"), retention }),
    async close() { store.close(); await rm(directory, { recursive: true, force: true }); },
  };
}

test("creates, hashes, lists, and independently verifies an online backup", async () => {
  const fx = await fixture();
  try {
    const backup = await fx.service.createBackup({ createdBy: "USR-OWNER" });
    assert.match(backup.id, /^BKP-\d{8}T\d{6}-[a-f0-9]{12}$/);
    assert.equal(backup.integrity, "ok");
    assert.equal(backup.sha256.length, 64);
    assert.equal(backup.requiredTables, 11);
    assert.ok(backup.size > 0);
    assert.equal(backup.path, undefined);
    assert.deepEqual((await fx.service.listBackups()).map((item) => item.id), [backup.id]);
    const verified = await fx.service.verifyBackup(backup.id);
    assert.equal(verified.integrity, "ok");
    assert.equal(verified.sha256, backup.sha256);
    assert.ok(verified.verifiedAt);
  } finally {
    await fx.close();
  }
});

test("enforces retention without accepting path traversal backup ids", async () => {
  const fx = await fixture(2);
  try {
    await fx.service.createBackup();
    await fx.service.createBackup();
    await fx.service.createBackup();
    assert.equal((await fx.service.listBackups()).length, 2);
    await assert.rejects(() => fx.service.verifyBackup("../../platform"), (error) => error.code === "INVALID_BACKUP_ID");
  } finally {
    await fx.close();
  }
});
