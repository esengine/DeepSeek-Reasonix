// Run: tsx src/__tests__/local-path-links.test.tsx
//
// Recognition matrix for chat markdown local-path linkification (issue
// #7426): Windows drive paths, UNC paths and file:/// URLs become clickable
// links; sentence punctuation and non-path lookalikes must not be captured.
// Also covers the file:/// href round-trip used by RichMarkdownLink.

import { linkifyLocalPaths, localPathHref, remarkLocalPathLinks } from "../lib/localPathLinks";
import { localPathFromHref } from "../components/githubLink";

let passed = 0;
let failed = 0;

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

function ok(value: unknown, label: string) {
  eq(Boolean(value), true, label);
}

function firstPath(text: string): string | undefined {
  return linkifyLocalPaths(text).find((s) => s.path !== undefined)?.path;
}

function pathCount(text: string): number {
  return linkifyLocalPaths(text).filter((s) => s.path !== undefined).length;
}

console.log("\nlinkifyLocalPaths — drive paths");

eq(firstPath("文件在 D:\\Project\\Jhtj\\20250804_000000_001_中停时分析\\05-静态验收.md 已生成"),
  "D:\\Project\\Jhtj\\20250804_000000_001_中停时分析\\05-静态验收.md",
  "issue case: Chinese dirs and underscores survive");
eq(firstPath("see D:\\a\\b c.md note"), "D:\\a\\b", "unescaped space still ends the path");
eq(firstPath("see D:\\a\\b\\ c.md"), "D:\\a\\b c.md", "escaped space unescapes to the real path");
eq(firstPath("saved C:/Users/me/AppData/Local/x.md here"), "C:/Users/me/AppData/Local/x.md",
  "forward-slash drive path");
eq(firstPath("see D:\\x\\y.md. done"), "D:\\x\\y.md", "trailing period stripped");
eq(firstPath("path D:\\x\\y.md, ok"), "D:\\x\\y.md", "trailing comma stripped");
eq(firstPath("see D:\\x\\y.md) done"), "D:\\x\\y.md", "trailing closing paren stripped");
eq(firstPath("see D:\\x\\y.md... done"), "D:\\x\\y.md", "trailing ellipsis stripped");
eq(firstPath("see D:\\x\\y.md。打开它"), "D:\\x\\y.md", "CJK period stops the path");
eq(firstPath("见 D:\\x\\y.md，查看详情"), "D:\\x\\y.md", "CJK comma stops the path");
eq(firstPath("打开 D:\\x\\y.md; 完成"), "D:\\x\\y.md", "semicolon stops the path");
eq(firstPath("见 D:\\x\\y.md（已生成）"), "D:\\x\\y.md", "full-width parens stop the path");
eq(firstPath("报告 D:\\x\\y.md（终版）完成"), "D:\\x\\y.md", "full-width paren group after path");

console.log("\nlinkifyLocalPaths — file:/// and UNC");

eq(firstPath("see file:///D:/Project/report/05-final.md"), "file:///D:/Project/report/05-final.md",
  "file URL matched whole, drive part not double-matched");
eq(firstPath("见 file:///D:/Project/中停时分析/05-静态验收.md"), "file:///D:/Project/中停时分析/05-静态验收.md",
  "file URL with CJK dirs");
eq(firstPath("see file:///D:/x/y.md. done"), "file:///D:/x/y.md", "file URL trailing period stripped");
eq(firstPath("see \\nas\\share\\docs\\report.md done"), "\\\\nas\\share\\docs\\report.md",
  "UNC path matched whole and \\\\ prefix restored");

console.log("\nlinkifyLocalPaths — lookalikes and non-paths");

eq(pathCount("C: drive"), 0, "bare drive letter is not a path");
eq(pathCount("at 10:30 now"), 0, "clock time is not a path");
eq(pathCount("version 1.2.3 released"), 0, "version string is not a path");
eq(pathCount("http://example.com 正常"), 0, "http URL untouched (GFM handles it)");
eq(pathCount("prefixD:\\x\\y.md"), 0, "drive path after a letter is rejected without lookbehind");
eq(pathCount("file:///D:/x/y.md"), 1, "file URL drive segment is not double-matched");
eq(pathCount("a\\b\\c.md 讨论"), 0, "share-less backslash path in prose is not a UNC path");
eq(pathCount("C:\\nas\\share\\x.md"), 1, "drive path does not also become a UNC match");
eq(firstPath("见 \\nas\\share\\x.md"), "\\\\nas\\share\\x.md", "real UNC path still matches");
eq(firstPath("见 C:\\Program\\ Files\\ (x86) 完成"), "C:\\Program Files (x86)", "escaped-space path with (x86) keeps its closing paren");

console.log("\nsegment alternation");

const segs = linkifyLocalPaths("见 D:\\x\\y.md 然后 file:///C:/a/b.txt 完成");
eq(segs.length, 5, "text/link/text/link/text alternation");
eq(segs[0], { text: "见 " }, "leading plain text");
eq(segs[1].path, "D:\\x\\y.md", "first link segment");
eq(segs[2], { text: " 然后 " }, "middle plain text");
eq(segs[3].path, "file:///C:/a/b.txt", "second link segment");
eq(segs[4], { text: " 完成" }, "trailing plain text");

console.log("\nlocalPathHref round-trip");

eq(localPathHref("D:\\x\\y.md"), "file:///D:/x/y.md", "backslashes become forward slashes");
eq(localPathHref("D:\\Project\\中停时分析\\05-静态验收.md"), "file:///D:/Project/%E4%B8%AD%E5%81%9C%E6%97%B6%E5%88%86%E6%9E%90/05-%E9%9D%99%E6%80%81%E9%AA%8C%E6%94%B6.md",
  "CJK percent-encoded");
eq(localPathHref("D:\\a\\b#c.md"), "file:///D:/a/b%23c.md", "hash escaped for URL safety");
eq(localPathHref("file:///C:/a/b.txt"), "file:///C:/a/b.txt", "already-absolute file URL is not double-prefixed");
eq(localPathHref("file:///D:/a%20b.txt"), "file:///D:/a%20b.txt", "literal %xx in a file URL is not double-encoded");
eq(localPathFromHref("file:///D:/a%20b.txt"), "D:/a b.txt", "decoded %20 becomes a real space");

eq(localPathFromHref("file:///D:/x/y.md"), "D:/x/y.md", "decodes plain path");
eq(localPathFromHref("file:///D:/Project/%E4%B8%AD%E5%81%9C%E6%97%B6%E5%88%86%E6%9E%90/05-%E9%9D%99%E6%80%81%E9%AA%8C%E6%94%B6.md"),
  "D:/Project/中停时分析/05-静态验收.md", "decodes percent-encoded CJK");
eq(localPathFromHref("file:///D:/x/%zz"), null, "malformed escapes yield null");
eq(localPathFromHref("https://example.com/x"), null, "http URL is not local");
eq(localPathFromHref(undefined), null, "undefined href is not local");

console.log("\nremark plugin via react-markdown render");

import React from "react";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import { renderToStaticMarkup } from "react-dom/server";

// Mirrors MarkdownRenderer: file:/// anchors survive the default sanitizer.
const markdownUrlTransform = (value: string) =>
  value.startsWith("file:///") ? value : defaultUrlTransform(value);

const html = renderToStaticMarkup(
  <ReactMarkdown remarkPlugins={[remarkLocalPathLinks]} urlTransform={markdownUrlTransform}>
    {"文件在 D:\\Project\\x\\05-静态验收.md 已生成"}
  </ReactMarkdown>,
);
ok(html.includes('href="file:///D:/Project/x/05-%E9%9D%99%E6%80%81%E9%AA%8C%E6%94%B6.md"'),
  "plain drive path renders as a file:/// anchor");
ok(html.includes("05-静态验收.md"), "anchor label keeps the original text");

const mixed = renderToStaticMarkup(
  <ReactMarkdown remarkPlugins={[remarkLocalPathLinks]} urlTransform={markdownUrlTransform}>
    {"见 D:\\x\\y.md，然后 file:///C:/a/b.txt 完成"}
  </ReactMarkdown>,
);
ok(mixed.includes('href="file:///D:/x/y.md"'), "CJK comma boundary renders drive link");
ok(mixed.includes('href="file:///C:/a/b.txt"'), "explicit file URL renders its own anchor");

const plain = renderToStaticMarkup(
  <ReactMarkdown remarkPlugins={[remarkLocalPathLinks]} urlTransform={markdownUrlTransform}>
    {"no paths here"}
  </ReactMarkdown>,
);
ok(!plain.includes("<a"), "plain text without paths renders no anchors");

const nested = renderToStaticMarkup(
  <ReactMarkdown remarkPlugins={[remarkLocalPathLinks]} urlTransform={markdownUrlTransform}>
    {"[D:\\x\\y.md](https://example.com)"}
  </ReactMarkdown>,
);
ok(!nested.includes("file:///"), "explicit link text is not re-linkified (no nested anchors)");

const uppercase = renderToStaticMarkup(
  <ReactMarkdown remarkPlugins={[remarkLocalPathLinks]} urlTransform={markdownUrlTransform}>
    {"see FILE:///C:/x/y.md"}
  </ReactMarkdown>,
);
ok(!uppercase.includes("file:///"), "uppercase FILE:/// stays inert (case-sensitive allowlist)");

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
