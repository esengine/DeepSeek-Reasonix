// Run: tsx src/__tests__/draft-credentials.test.ts

import { draftIDsFromSubmit } from "../lib/draftCredentials";
import { readSessionAttachmentDataURL } from "../lib/sessionAttachmentRead";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ndraft credentials");
eq(
  draftIDsFromSubmit("inspect @[a.png](draft:0123456789abcdef0123456789abcdef) and @[b.png](draft:0123456789ABCDEF0123456789ABCDEF)"),
  ["0123456789abcdef0123456789abcdef"],
  "extracts unique lowercase draft credentials",
);
eq(draftIDsFromSubmit("no drafts here"), [], "returns no ids without draft credentials");

console.log("\nsession attachment read");
const png = Uint8Array.from([1, 2, 3, 4]);
const encoded = Buffer.from(png).toString("base64");
const url = await readSessionAttachmentDataURL("tab-1", "ab".repeat(32), "image/png", async (_tab, _digest, offset) => {
  if (offset !== 0) throw new Error(`unexpected offset ${offset}`);
  return { data: encoded, nextOffset: png.length, done: true };
});
eq(url, `data:image/png;base64,${encoded}`, "assembles a bounded attachment preview");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
