// Run: tsx src/__tests__/ref-token.test.tsx
//
// Tests for the @-token escape grammar (lib/refToken) and the composer/file
// menu helpers that produce and consume it. Mirrors internal/control/refs.go:
// backslash-escaped space/tab stays in the token; other backslashes are
// literal so Windows separators keep their meaning.

import { escapeRefPath, unescapeRefPath, activeRefTokenRe } from "../lib/refToken";
import { activeFileReferenceToken, pickInlineFileReference } from "../components/FileReferenceMenu";
import { composerPickFileEntry } from "../components/Composer";
import type { DirEntry } from "../lib/types";

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

console.log("\nref token grammar");

eq(escapeRefPath("my file.txt"), "my\\ file.txt", "escapeRefPath escapes spaces");
eq(escapeRefPath("plain.txt"), "plain.txt", "escapeRefPath leaves space-free paths alone");
eq(unescapeRefPath("my\\ file.txt"), "my file.txt", "unescapeRefPath reverses escaping");
eq(unescapeRefPath("C:\\dir\\file.png"), "C:\\dir\\file.png", "unescapeRefPath keeps Windows separators literal");
eq(unescapeRefPath(escapeRefPath("a b\tc.md")), "a b\tc.md", "escape/unescape round-trips");

eq(activeRefTokenRe.exec("see @docs/my\\ file.md")?.[1], "docs/my\\ file.md", "active token keeps escaped spaces");
eq(activeRefTokenRe.exec("see @docs/x plain")?.[1], undefined, "unescaped space still ends the token");

console.log("\nfile reference menu");

eq(
  activeFileReferenceToken("look at @docs/my\\ dir/fra"),
  { raw: "docs/my\\ dir/fra", dir: "docs/my\\ dir/", frag: "fra" },
  "token splits dir/frag with escapes intact",
);
eq(activeFileReferenceToken("@my\\ f")?.frag, "my f", "frag is unescaped for entry matching");

const spacedFile: DirEntry = { name: "my file.md", isDir: false };
eq(
  pickInlineFileReference("see @docs/", "docs/", "docs/", spacedFile),
  "see @docs/my\\ file.md ",
  "inline pick escapes whitespace in the inserted ref",
);
eq(
  pickInlineFileReference("see @docs/my\\ dir/", "docs/my\\ dir/", "docs/my\\ dir/", spacedFile),
  "see @docs/my\\ dir/my\\ file.md ",
  "inline pick through an escaped dir stays escaped once",
);

console.log("\ncomposer pick");

const inline = composerPickFileEntry("note @sr", "sr", "", { name: "my report.pdf", isDir: false });
eq(inline.text, "note @my\\ report.pdf ", "composer inline fallback escapes whitespace");
eq(inline.workspaceRef, undefined, "no structured ref for a name-only entry");

const structured = composerPickFileEntry("note @sr", "sr", "", {
  name: "my report.pdf",
  path: "docs/my report.pdf",
  isDir: false,
});
eq(structured.workspaceRef?.path, "docs/my report.pdf", "structured ref keeps the real unescaped path");
eq(structured.text, "note ", "structured pick removes the typed token");

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
