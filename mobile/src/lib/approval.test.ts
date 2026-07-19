import assert from "node:assert/strict";
import { isDangerousWrite, riskFromTool } from "./approval.ts";

function main() {
  // Snake_case tool names must classify the same as their spaced forms.
  assert.equal(riskFromTool("delete_file", "tmp/scratch.log"), "high");
  assert.equal(riskFromTool("write_file", "src/example.ts"), "medium");
  assert.equal(riskFromTool("shell", "sudo rm -rf build/"), "high");
  assert.equal(riskFromTool("git-push", "origin main"), "medium");
  assert.equal(riskFromTool("read_file", "src/app.ts"), "low");

  assert.equal(isDangerousWrite("high", "shell"), true);
  assert.equal(isDangerousWrite("medium", "write_file"), true);
  assert.equal(isDangerousWrite("medium", "mv"), false);
  assert.equal(isDangerousWrite("low", "read_file"), false);

  console.log("approval.test.ts: ok");
}

main();
