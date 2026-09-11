#!/usr/bin/env node
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import ts from "typescript";

const HOST_GLOBALS = new Set(["go", "runtime", "reasonixDesktop"]);
const SHELL_MODULES = [/(?:^|\/)wailsjs(?:\/|$)/, /^@wailsapp(?:\/|$)/];
const ALLOWED = new Set(["lib/desktopHost.ts"]);

function sourceFiles(root) {
  if (!existsSync(root)) return [];
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory() ? sourceFiles(join(root, entry.name))
      : /\.[cm]?[jt]sx?$/.test(entry.name) ? [join(root, entry.name)] : []);
}

function unwrap(node) {
  while (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isTypeAssertionExpression(node)
    || ts.isNonNullExpression(node) || ts.isSatisfiesExpression(node)) node = node.expression;
  return node;
}

function isWindowObject(node) {
  const inner = unwrap(node);
  if (ts.isIdentifier(inner)) return inner.text === "window";
  // globalThis.window / self.window
  return ts.isPropertyAccessExpression(inner) && inner.name.text === "window" && ts.isIdentifier(unwrap(inner.expression));
}

function line(tree, node) {
  return tree.getLineAndCharacterOfPosition(node.getStart(tree)).line + 1;
}

export function desktopHostViolations(code, file) {
  const tree = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true);
  const violations = [];
  const shellModule = (specifier) => SHELL_MODULES.some((pattern) => pattern.test(specifier));
  function visit(node) {
    if (ts.isPropertyAccessExpression(node) && HOST_GLOBALS.has(node.name.text) && isWindowObject(node.expression)) {
      violations.push(`${line(tree, node)}: window.${node.name.text}`);
    } else if (ts.isElementAccessExpression(node) && ts.isStringLiteral(node.argumentExpression)
      && HOST_GLOBALS.has(node.argumentExpression.text) && isWindowObject(node.expression)) {
      violations.push(`${line(tree, node)}: window["${node.argumentExpression.text}"]`);
    } else if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier) && shellModule(node.moduleSpecifier.text)) {
      // The Wails shell is retired; no import of its generated bindings or
      // runtime package is legitimate anymore, type-only included.
      violations.push(`${line(tree, node)}: import from ${node.moduleSpecifier.text}`);
    } else if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)
      && shellModule(node.moduleSpecifier.text) && !node.isTypeOnly) {
      violations.push(`${line(tree, node)}: export from ${node.moduleSpecifier.text}`);
    } else if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword
      || (ts.isIdentifier(node.expression) && node.expression.text === "require"))) {
      const argument = node.arguments[0];
      if (argument && ts.isStringLiteral(argument) && shellModule(argument.text)) {
        violations.push(`${line(tree, node)}: dynamic import of ${argument.text}`);
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  return violations;
}

export function checkDesktopHostBoundary(sourceRoot) {
  const failures = [];
  for (const file of sourceFiles(sourceRoot)) {
    const name = relative(sourceRoot, file).replaceAll("\\", "/");
    if (ALLOWED.has(name) || name.startsWith("__tests__/") || /\.test\.[cm]?[jt]sx?$/.test(name)) continue;
    for (const violation of desktopHostViolations(readFileSync(file, "utf8"), file)) failures.push(`${name}:${violation}`);
  }
  return failures;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const frontend = dirname(dirname(fileURLToPath(import.meta.url)));
  const failures = checkDesktopHostBoundary(join(frontend, "src"));
  for (const failure of failures) console.error("check-desktop-host-boundary: " + failure);
  if (failures.length) process.exitCode = 1;
  else console.log("check-desktop-host-boundary: only lib/desktopHost.ts reaches the shell globals");
}
