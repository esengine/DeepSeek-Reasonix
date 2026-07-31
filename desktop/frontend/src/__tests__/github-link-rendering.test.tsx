// Run: tsx src/__tests__/github-link-rendering.test.tsx

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { classifyLinkIcon, parseGitHubLink } from "../components/githubLink";
import { RichMarkdownLink } from "../components/githubLink";

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

console.log("\ngithub link rendering");

eq(parseGitHubLink("https://github.com/esengine/DeepSeek-Reasonix/issues/6856"), {
  kind: "issue",
  owner: "esengine",
  repo: "DeepSeek-Reasonix",
  value: "6856",
  compactLabel: "#6856",
}, "recognizes GitHub issue links");

eq(parseGitHubLink("https://github.com/esengine/DeepSeek-Reasonix/pull/123?diff=split"), {
  kind: "pull",
  owner: "esengine",
  repo: "DeepSeek-Reasonix",
  value: "123",
  compactLabel: "PR #123",
}, "recognizes GitHub pull request links with query parameters");

eq(parseGitHubLink("https://github.com/esengine/DeepSeek-Reasonix/commit/abcdef1234567890"), {
  kind: "commit",
  owner: "esengine",
  repo: "DeepSeek-Reasonix",
  value: "abcdef1234567890",
  compactLabel: "abcdef1",
}, "recognizes GitHub commit links");

eq(parseGitHubLink("https://github.com/esengine/DeepSeek-Reasonix"), null, "leaves repository links unchanged");
eq(parseGitHubLink("http://github.com/esengine/DeepSeek-Reasonix/issues/1"), null, "rejects insecure GitHub links");
eq(parseGitHubLink("https://example.com/issues/6856"), null, "leaves non-GitHub links unchanged");
eq(parseGitHubLink("javascript:alert(1)"), null, "does not enhance unsafe links");

eq(classifyLinkIcon("https://github.com/esengine/DeepSeek-Reasonix"), "github", "adds a GitHub icon to repository links");
eq(classifyLinkIcon("https://example.com/docs"), "external", "adds an external-link icon to web links");
eq(classifyLinkIcon("http://localhost:5173/docs"), "external", "adds an external-link icon to HTTP links");
eq(classifyLinkIcon("mailto:hello@example.com"), "mail", "adds a mail icon to email links");
eq(classifyLinkIcon("./docs/GUIDE.md"), null, "leaves relative links without an icon");
eq(classifyLinkIcon("#section"), null, "leaves page fragments without an icon");
eq(classifyLinkIcon("javascript:alert(1)"), null, "does not add icons to unsafe protocols");

function renderLink(href: string, label: string = href) {
  const markup = renderToStaticMarkup(
    createElement(RichMarkdownLink, { href, children: label }),
  );
  return new JSDOM(markup).window.document.querySelector("a");
}

{
  const href = "https://github.com/esengine/DeepSeek-Reasonix/pull/6976";
  const link = renderLink(href);
  const label = link?.querySelector(".md-rich-link__label");
  eq(label?.textContent, href, "keeps the original GitHub URL in selectable DOM text");
  eq(label?.getAttribute("data-display-label"), "PR #6976", "exposes the compact visual PR label");
  eq(
    link?.getAttribute("aria-label"),
    "GitHub esengine/DeepSeek-Reasonix pull request #6976",
    "gives compact PR links a contextual accessible name",
  );
  ok(link?.querySelector("svg[aria-hidden=\"true\"]"), "hides the decorative GitHub icon from assistive technology");

  const selection = link?.ownerDocument.defaultView?.getSelection();
  const range = link?.ownerDocument.createRange();
  if (link && selection && range) {
    range.selectNodeContents(link);
    selection.removeAllRanges();
    selection.addRange(range);
  }
  eq(selection?.toString(), href, "copies the original URL instead of the compact visual label");
}

{
  const link = renderLink(
    "https://github.com/esengine/DeepSeek-Reasonix/issues/6856",
    "Readable issue title",
  );
  eq(link?.textContent, "Readable issue title", "preserves an explicit Markdown link label");
  eq(
    link?.querySelector(".md-rich-link__label")?.hasAttribute("data-display-label"),
    false,
    "does not compact explicit Markdown link labels",
  );
  eq(link?.hasAttribute("aria-label"), false, "does not override an explicit accessible link label");
}

{
  const link = renderLink("https://example.com/a/very/long/documentation/path");
  ok(link?.classList.contains("md-rich-link--external"), "renders ordinary HTTPS destinations as rich external links");
  ok(link?.querySelector("svg[aria-hidden=\"true\"]"), "renders a decorative external-link icon");
}

{
  const relative = renderLink("./docs/GUIDE.md", "Guide");
  eq(relative?.className, "", "leaves relative links without rich-link styling");
  eq(relative?.querySelector("svg"), null, "leaves relative links without an icon");
}

{
  const styles = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
  ok(
    /\.md \.md-rich-link\s*\{[^}]*white-space:\s*normal;/.test(styles),
    "allows rich links to wrap on narrow layouts",
  );
  ok(
    /\.md \.md-rich-link__label\s*\{[^}]*min-width:\s*0;[^}]*overflow-wrap:\s*anywhere;/.test(styles),
    "lets long unbroken link labels shrink and wrap",
  );
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
