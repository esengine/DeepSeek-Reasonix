import assert from "node:assert/strict";
import {
  LOCAL_CAPABILITIES,
  MOBILE_PROTOCOL_VERSION,
  isWriteCommand,
  newEnvelope,
} from "./types.ts";

function main() {
  const env = newEnvelope("ping");
  assert.equal(env.version, MOBILE_PROTOCOL_VERSION);
  assert.equal(env.type, "ping");
  assert.equal(env.requestId, undefined);

  assert.equal(isWriteCommand("submit"), true);
  assert.equal(isWriteCommand("list_models"), false);

  assert.deepEqual([...LOCAL_CAPABILITIES], [
    "web_read",
    "attachment_read",
    "image_input",
    "http_mcp",
  ]);

  console.log("protocol.test.ts: ok");
}

main();
