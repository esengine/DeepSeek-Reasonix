// Run: tsx src/__tests__/code-block-copy-position.test.tsx

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import HljsCode from "../components/editors/HljsCode";
import { LocaleProvider } from "../lib/i18n";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function notOk(value: unknown, label: string) {
  ok(!value, label);
}

function renderCodeBlock() {
  return renderToStaticMarkup(
    createElement(
      LocaleProvider,
      null,
      createElement(HljsCode, {
        value: "const veryLongLine = 'x'.repeat(240);",
        language: "ts",
        maxHeight: 120,
      }),
    ),
  );
}

function selectorPresent(css: string, selector: string): boolean {
  return css.includes(selector);
}

console.log("\ncode block copy position");

const markup = renderCodeBlock();
const dom = new JSDOM(markup);
const document = dom.window.document;
const wrap = document.querySelector(".code-block__wrap");
const pre = document.querySelector("pre.code.hljs");
const copy = document.querySelector(".code-block__copy");

ok(wrap, "HljsCode renders a positioned copy wrapper");
ok(pre, "HljsCode renders the highlighted pre");
ok(copy, "HljsCode renders the copy button");
ok(pre?.parentElement === wrap, "scrollable pre is inside the wrapper");
ok(copy?.parentElement === wrap, "copy button is a wrapper child");
notOk(pre?.contains(copy ?? null), "copy button is outside the scrollable pre");
ok(copy?.previousElementSibling === pre, "copy button follows pre as a sibling");

const stylesPath = fileURLToPath(new URL("../styles.css", import.meta.url));
const css = readFileSync(stylesPath, "utf8");

ok(selectorPresent(css, ".code-block__wrap:hover .code-block__copy"), "hover reveal targets the wrapper");
notOk(selectorPresent(css, ".code:hover .code-block__copy"), "hover reveal no longer depends on pre descendants");
ok(
  selectorPresent(css, ".app--creation .msg--assistant .md .code:not([data-lang]) + .code-block__copy"),
  "creation unlabelled code button styles target sibling copy buttons",
);
notOk(
  selectorPresent(css, ".app--creation .msg--assistant .md .code:not([data-lang]) .code-block__copy"),
  "creation unlabelled code styles do not target descendants inside pre",
);
ok(
  selectorPresent(css, '.app--creation .msg--assistant .md .code[data-lang="bash"] + .code-block__copy'),
  "creation shell code button styles target sibling copy buttons",
);
notOk(
  selectorPresent(css, '.app--creation .msg--assistant .md .code[data-lang="bash"] .code-block__copy'),
  "creation shell code styles do not target descendants inside pre",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
