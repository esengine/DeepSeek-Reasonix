// Run: tsx src/__tests__/plain-text-links.test.tsx

import { Fragment, createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { linkifyPlainText } from "../components/plainTextLinks";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  eq(Boolean(value), true, label);
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(
      `  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`,
    );
    failed += 1;
  }
}

function render(text: string): string {
  return renderToStaticMarkup(createElement(Fragment, null, ...linkifyPlainText(text)));
}

console.log("\nplain text links");

const simple = render("see https://example.com/x now");
ok(
  simple.includes('<a class="msg__text-link" href="https://example.com/x" title="https://example.com/x">https://example.com/x</a>'),
  "bare URL becomes a clickable anchor",
);
ok(simple.includes("see ") && simple.includes(" now"), "surrounding text is preserved");

eq(render("no links here"), "no links here", "plain text without URLs passes through untouched");
eq(render(""), "", "empty text stays empty");

// Trailing punctuation is trimmed from the destination while the visible
// spelling keeps the original punctuation.
const punct = render("visit https://example.com/a, and https://example.com/b!");
ok(
  punct.includes('href="https://example.com/a"'),
  "comma is trimmed from the first destination",
);
ok(
  punct.includes(">https://example.com/a,<"),
  "comma stays visible in the label",
);
ok(
  punct.includes('href="https://example.com/b"'),
  "exclamation mark is trimmed from the second destination",
);
ok(
  punct.includes(">https://example.com/b!<"),
  "exclamation mark stays visible in the label",
);

// CJK full-width punctuation is trimmed too.
const cjk = render("看 https://example.com/文档。");
ok(
  cjk.includes('href="https://example.com/文档"'),
  "full-width period is trimmed from the destination",
);
ok(cjk.includes("。"), "full-width period stays visible in the label");

// Multiple URLs, http and https.
const multi = render("a https://a.com b http://b.com c");
ok(
  multi.includes('href="https://a.com"') && multi.includes('href="http://b.com"'),
  "multiple URLs are all linkified",
);

// Quotes and angle brackets terminate a URL like the TUI regex.
const quoted = render('say "https://x.com/y" ok');
ok(quoted.includes('href="https://x.com/y"'), "quotes terminate the URL");
ok(quoted.includes(">https://x.com/y<"), "quoted URL keeps its label");

// Email addresses and bare domains are not linkified (keeps user bubbles plain).
const noScheme = render("mail me at a@b.com or example.com");
ok(!noScheme.includes("<a"), "emails and bare domains stay plain text");

// Non-http schemes are not linkified.
const ftp = render("ftp://example.com/file");
ok(!ftp.includes("<a"), "ftp URLs stay plain text");

console.log(`\nplain text links: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
