// Run: tsx src/__tests__/language-highlighting.test.ts

import { resolveLang } from "../lib/highlight";
import {
  FILE_LANGUAGE_BY_EXTENSION,
  FILE_LANGUAGE_BY_NAME,
  extToLang,
  pathToLang,
} from "../lib/lang";
import { formatSelectionReference, languageFor } from "../lib/selectedTextContext";

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

console.log("\nlanguage highlighting");

const pathCases = [
  ["src/App.cs", "csharp", "csharp"],
  ["src/Component.tsx", "tsx", "typescript"],
  ["src/view.xaml", "xaml", "xml"],
  ["src/index.html", "html", "xml"],
  ["config/app.toml", "toml", "ini"],
  ["Dockerfile", "dockerfile", "dockerfile"],
  ["containers/Dockerfile.dev", "dockerfile", "dockerfile"],
  ["C:\\repo\\Containerfile.windows", "dockerfile", "dockerfile"],
  ["Makefile", "makefile", "makefile"],
  ["build/GNUmakefile.release", "makefile", "makefile"],
  ["CMakeLists.txt", "cmake", "cmake"],
  ["public/.htaccess", "apache", "apache"],
  ["config/httpd.conf", "apache", "apache"],
  ["config/nginx.conf", "nginx", "nginx"],
  ["build/module.makefile", "makefile", "makefile"],
] as const;

for (const [path, semantic, lexer] of pathCases) {
  eq(pathToLang(path), semantic, `${path} has one semantic path mapping`);
  eq(extToLang(path), semantic, `${path} keeps tool diffs consistent`);
  eq(languageFor(path), semantic, `${path} keeps file previews consistent`);
  eq(resolveLang(semantic), lexer, `${path} resolves to a registered lexer`);
}

const mappedLanguages = new Set([
  ...Object.values(FILE_LANGUAGE_BY_EXTENSION),
  ...Object.values(FILE_LANGUAGE_BY_NAME),
]);
for (const language of mappedLanguages) {
  eq(resolveLang(language) !== "", true, `${language} mapping has a registered lexer`);
}

for (const [path, fenceTag] of [
  ["Component.tsx", "tsx"],
  ["template.html", "html"],
  ["settings.toml", "toml"],
  ["View.xaml", "xaml"],
] as const) {
  const fenceLine = formatSelectionReference(path, "sample").split("\n")[2];
  eq(fenceLine, `\`\`\`${fenceTag}`, `${path} preserves its provider-visible semantic fence tag`);
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
