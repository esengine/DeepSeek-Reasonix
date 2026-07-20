// Run: tsx src/__tests__/markdown-image-proxy.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { markdownImageSource, REMOTE_MARKDOWN_IMAGE_PATH } from "../lib/markdownImage";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nmarkdown image proxy");

eq(
  markdownImageSource("https://cdn.example.com/a b.png?size=2", true),
  `${REMOTE_MARKDOWN_IMAGE_PATH}?url=${encodeURIComponent("https://cdn.example.com/a b.png?size=2")}`,
  "native shell routes HTTPS images through the backend endpoint",
);
eq(
  markdownImageSource("//cdn.example.com/pixel.png", true),
  `${REMOTE_MARKDOWN_IMAGE_PATH}?url=${encodeURIComponent("https://cdn.example.com/pixel.png")}`,
  "native shell routes protocol-relative images through the backend endpoint",
);
eq(markdownImageSource("data:image/png;base64,AAAA", true), "data:image/png;base64,AAAA", "data images stay local");
eq(markdownImageSource("/__reasonix_workspace_media/token/a.png", true), "/__reasonix_workspace_media/token/a.png", "workspace images stay local");
eq(markdownImageSource("https://cdn.example.com/a.png", false), "https://cdn.example.com/a.png", "browser development keeps direct image URLs");

const testDir = dirname(fileURLToPath(import.meta.url));
const rendererSource = readFileSync(resolve(testDir, "../components/MarkdownRenderer.tsx"), "utf8");
eq(rendererSource.includes("img: ({ src, alt, title })"), true, "Markdown renderer owns the image element");
eq(rendererSource.includes("markdownImageSource(src)"), true, "Markdown renderer applies backend routing");

if (failed > 0) {
  process.stderr.write(`\n${failed} markdown image proxy assertion(s) failed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} markdown image proxy assertions passed\n`);
