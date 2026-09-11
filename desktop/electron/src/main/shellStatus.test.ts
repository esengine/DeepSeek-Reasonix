import assert from "node:assert/strict";
import { test } from "node:test";
import { createConnection } from "node:net";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { once } from "node:events";
import { initialShellStatus, listenShellStatus, homeKey } from "./shellStatus.js";

test("status survives a dead service and is bounded to a read-only snapshot", async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "shell-status-"));
  const address = process.platform === "win32" ? `\\\\.\\pipe\\reasonix-test-${process.pid}` : join(dir, "status.sock");
  const status = initialShellStatus("C:\\Users\\Test\\desktop-shell", "v1.38.7");
  status.lifecycle = "failed"; status.service = "exited";
  const server = listenShellStatus(() => status, { info() {}, warn() {}, error() {} }, address);
  t.after(() => { server.close(); rmSync(dir, { recursive: true, force: true }); });
  await once(server, "listening");
  const socket = createConnection(address);
  let text = ""; socket.on("data", (data) => { text += data; });
  await once(socket, "end");
  assert.deepEqual(JSON.parse(text), status);
  assert.equal(homeKey("C:\\Users\\TEST\\desktop-shell"), homeKey("c:/users/test/desktop-shell"));
  assert.equal(Object.hasOwn(status, "token"), false);
});
