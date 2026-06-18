// Run: tsx src/__tests__/bundle-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const markdownSource = readFileSync(resolve(here, "../components/Markdown.tsx"), "utf8");

console.log("\nbundle contract");

ok(
  !/import\s+\{[^}]*\}\s+from\s+["']\.\/lib\/sessionExport["']/.test(appSource),
  "App keeps session export code out of the initial chunk",
);
ok(
  appSource.includes('import("./lib/sessionExport")'),
  "App loads session export code on demand",
);
ok(
  !/import\s+\{[^}]*\}\s+from\s+["']\.\/components\/SettingsPanel["']/.test(appSource) &&
    !/import\s+\{[^}]*\}\s+from\s+["']\.\/components\/HistoryPanel["']/.test(appSource),
  "App keeps secondary drawers out of the initial chunk",
);
ok(
  appSource.includes('import("./components/SettingsPanel")') &&
    appSource.includes('import("./components/HistoryPanel")'),
  "App loads secondary drawers on demand",
);
ok(
  !/from\s+["']react-markdown["']/.test(markdownSource) &&
    !/from\s+["']remark-gfm["']/.test(markdownSource) &&
    !/from\s+["']remark-math["']/.test(markdownSource) &&
    !/from\s+["']rehype-katex["']/.test(markdownSource) &&
    !/katex\/dist\/katex\.min\.css/.test(markdownSource),
  "Markdown wrapper keeps markdown/math vendor code out of the initial chunk",
);
ok(
  markdownSource.includes('import("./MarkdownRenderer")'),
  "Markdown wrapper loads markdown renderer on demand",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
