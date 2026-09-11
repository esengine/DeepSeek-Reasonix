import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { checkCssFile } from "./check-css-syntax.mjs";

const fixture = mkdtempSync(join(tmpdir(), "reasonix-css-syntax-"));
const check = (source) => {
  const file = join(fixture, "fixture.css");
  writeFileSync(file, source);
  return checkCssFile(file, "fixture.css");
};

try {
  // A selector list may span lines as long as every line but the last ends with
  // a comma, and a declaration value may wrap across lines. Neither is damage.
  assert.deepEqual(check(".a,\n.b,\n.c {\n  color: red;\n}\n"), []);
  assert.deepEqual(check(".a {\n  background: linear-gradient(\n    180deg,\n    red,\n    blue\n  );\n}\n"), []);
  assert.deepEqual(check("@media (max-width: 820px) {\n  .a { color: red; }\n}\n"), []);
  assert.deepEqual(check(".a {\n  color: red\n}\n"), [], "a final declaration may omit its semicolon");

  // The regression this guard exists for: a rule lost its declaration block, so
  // its selectors run into the next rule. The parser swallows the block that
  // follows and the damage never shows up in the browser.
  const glued = check(".workbench-dock__tabs\n\n.workbench-dock__tab\n\n@container (max-width: 520px) {\n  .x { width: 100%; }\n}\n");
  assert.equal(glued.length, 1);
  assert.match(glued[0], /fixture\.css:\d+:\d+/);
  assert.match(glued[0], /"\.workbench-dock__tabs"/);

  // Orphans glued to a plain style rule are reported at every site, not just
  // the first, so one run lists the whole repair.
  const several = check(".a\n\n.b\n\n.first { color: red; }\n.p, .q { color: blue; }\n.c\n\n.d\n\n.second { color: green; }\n");
  assert.equal(several.length, 2);
  assert.match(several[0], /"\.a"/);
  assert.match(several[1], /"\.c"/);

  // Delimiter damage still short-circuits before the selector pass.
  assert.match(check(".a { color: red;\n")[0], /opening brace/);
  assert.match(check(".a { color: red; }\n}\n")[0], /closing brace/);

  console.log("check-css-syntax: orphaned-selector guard contracts hold");
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
