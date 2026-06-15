// Run: tsx src/__tests__/attachment-display.test.ts

import { baseName, parseAttachmentRefsForDisplay, replaceAttachmentRefsForDisplay, sortDisplayAttachments } from "../lib/attachmentDisplay";

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

console.log("\nattachment display");

const named = parseAttachmentRefsForDisplay(
  "review @[DS30000.sl2](.reasonix/attachments/clipboard-20260610-121238.444775-000002.sl2) and @[park.png](.reasonix/attachments/clipboard-20260610-121238.444775-000001.png)",
);

eq(named.text, "review and", "removes named display refs from message text");
eq(
  named.attachments.map((a) => ({ path: a.path, name: a.name, kind: a.kind, ext: a.ext })),
  [
    {
      path: ".reasonix/attachments/clipboard-20260610-121238.444775-000002.sl2",
      name: "DS30000.sl2",
      kind: "file",
      ext: "SL2",
    },
    {
      path: ".reasonix/attachments/clipboard-20260610-121238.444775-000001.png",
      name: "park.png",
      kind: "image",
      ext: "PNG",
    },
  ],
  "preserves original display names for attachment cards",
);
eq(
  sortDisplayAttachments(named.attachments).map((a) => a.name),
  ["park.png", "DS30000.sl2"],
  "sorts images before files while keeping groups stable",
);
eq(
  replaceAttachmentRefsForDisplay("see @[DS30000.sl2](.reasonix/attachments/clipboard-20260610-121238.444775-000002.sl2)"),
  "see [file:DS30000.sl2]",
  "compact previews use named display refs",
);
eq(baseName("C:\\Users\\Abyss\\Desktop\\DS30000.sl2"), "DS30000.sl2", "extracts Windows path basenames");
eq(baseName("/Users/abyss/Desktop/park.png"), "park.png", "extracts POSIX path basenames");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
