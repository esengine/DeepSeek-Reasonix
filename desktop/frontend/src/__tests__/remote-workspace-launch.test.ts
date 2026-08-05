import { RemoteWorkspaceLaunchGate } from "../lib/remoteWorkspace";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nRemote workspace launch generations");

const gate = new RemoteWorkspaceLaunchGate();
const hostAFirst = gate.begin("host-a");
const hostBFirst = gate.begin("host-b");

ok(gate.isCurrent("host-a", hostAFirst), "opening host B does not cancel host A");
ok(gate.isCurrent("host-b", hostBFirst), "host B keeps its own current request");

const hostASecond = gate.begin("host-a");
ok(!gate.isCurrent("host-a", hostAFirst), "a newer request supersedes the same host");
ok(gate.isCurrent("host-a", hostASecond), "the latest same-host request stays current");
ok(gate.isCurrent("host-b", hostBFirst), "superseding host A does not cancel host B");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
