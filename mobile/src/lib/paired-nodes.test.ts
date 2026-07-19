import assert from "node:assert/strict";
import { buildPairingUri, parsePairingPayload } from "./paired-nodes.ts";

function main() {
  const uri = buildPairingUri({
    baseUrl: "http://127.0.0.1:8790",
    id: "node-a",
    name: "Dev",
    fingerprint: "abc123",
  });
  assert.ok(uri.startsWith("reasonix://node-pair?"));
  const parsed = parsePairingPayload(uri);
  assert.equal(parsed.baseUrl, "http://127.0.0.1:8790");
  assert.equal(parsed.id, "node-a");
  assert.equal(parsed.fingerprint, "abc123");

  const bare = parsePairingPayload("http://192.168.1.10:8790/");
  assert.equal(bare.baseUrl, "http://192.168.1.10:8790");

  const host = parsePairingPayload("localhost:8790");
  assert.equal(host.baseUrl, "http://localhost:8790");

  const json = parsePairingPayload(
    JSON.stringify({ url: "https://node.example:8790", name: "Remote", fp: "f1" }),
  );
  assert.equal(json.baseUrl, "https://node.example:8790");
  assert.equal(json.fingerprint, "f1");

  assert.throws(() => parsePairingPayload("not-a-url"), /unrecognized|empty/);

  console.log("paired-nodes.test.ts: ok");
}

main();
