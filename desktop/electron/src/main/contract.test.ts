import assert from "node:assert/strict";
import { test } from "node:test";
import { emptyContract, isAllowedCommand, parseContract } from "./contract.js";

test("parseContract accepts the command list in the shapes the generator may emit", () => {
  const fromStrings = parseContract({ digest: "sha256:a", commands: ["OpenProjectTab", "Submit"] });
  const fromObjects = parseContract({ digest: "sha256:a", protocolVersion: 1, commands: [{ name: "OpenProjectTab" }, { name: "Submit" }] });
  const fromRecord = parseContract({ digest: "sha256:a", commands: { OpenProjectTab: {}, Submit: {} } });
  for (const contract of [fromStrings, fromObjects, fromRecord]) {
    assert.deepEqual([...contract.commands], ["OpenProjectTab", "Submit"]);
    assert.equal(contract.digest, "sha256:a");
    assert.equal(contract.protocolVersion, 1);
  }
});

test("parseContract rejects a contract without a digest or without commands", () => {
  assert.throws(() => parseContract({ commands: ["A"] }), /digest/);
  assert.throws(() => parseContract({ digest: "sha256:a", commands: [] }), /no commands/);
  assert.throws(() => parseContract(null), /object/);
});

test("the allowlist admits exact command names only", () => {
  const contract = parseContract({ digest: "sha256:a", commands: ["OpenProjectTab"] });
  assert.equal(isAllowedCommand(contract, "OpenProjectTab"), true);
  assert.equal(isAllowedCommand(contract, "openprojecttab"), false);
  assert.equal(isAllowedCommand(contract, ""), false);
  assert.equal(isAllowedCommand(contract, "__proto__"), false);
  assert.equal(isAllowedCommand(contract, "constructor"), false);
  assert.equal(isAllowedCommand(contract, "toString"), false);
  assert.equal(isAllowedCommand(contract, 42), false);
  assert.equal(isAllowedCommand(emptyContract(), "OpenProjectTab"), false);
});
