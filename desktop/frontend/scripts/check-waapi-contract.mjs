#!/usr/bin/env node

import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";

const SOURCE_ROOT = "src";
const LEGACY_EASING_RE = /\b(?:power[0-4]|back|bounce|circ|elastic|expo|sine|strong)\.(?:in|out|inOut)\b/;
const CSS_EASING_RE = /^(?:linear|ease|ease-in|ease-out|ease-in-out|step-start|step-end|cubic-bezier\([^()]+\)|steps\([^()]+\)|linear\([^()]+\))$/;

function sourceFiles(root) {
  const files = [];
  const visit = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "__tests__") visit(path);
      } else if (/\.(?:ts|tsx)$/.test(entry.name) && !/\.test\.(?:ts|tsx)$/.test(entry.name)) {
        files.push(path);
      }
    }
  };
  visit(root);
  return files.sort();
}

function propertyName(node) {
  if (ts.isIdentifier(node) || ts.isStringLiteral(node)) return node.text;
  return "";
}

function validateSource(text, filename) {
  const portableFilename = filename.replaceAll("\\", "/");
  const source = ts.createSourceFile(
    filename,
    text,
    ts.ScriptTarget.Latest,
    true,
    filename.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const issues = [];
  const motionEasingImports = new Set();
  const isSharedMotionModule = /(?:^|\/)src\/lib\/motion\.ts$/.test(portableFilename);

  for (const statement of source.statements) {
    if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier)) continue;
    const moduleName = statement.moduleSpecifier.text;
    if (moduleName === "gsap" || moduleName === "@gsap/react") {
      issues.push(`${filename}:${source.getLineAndCharacterOfPosition(statement.getStart()).line + 1}: GSAP imports are forbidden in the native motion layer`);
    }
    if (!/(?:^|\/)motion$/.test(moduleName)) continue;
    const bindings = statement.importClause?.namedBindings;
    if (!bindings || !ts.isNamedImports(bindings)) continue;
    for (const element of bindings.elements) {
      const imported = element.propertyName?.text ?? element.name.text;
      if (imported.startsWith("CSS_EASE_")) motionEasingImports.add(element.name.text);
    }
  }
  if (isSharedMotionModule) {
    for (const statement of source.statements) {
      if (!ts.isVariableStatement(statement)) continue;
      for (const declaration of statement.declarationList.declarations) {
        if (!ts.isIdentifier(declaration.name) || !declaration.name.text.startsWith("CSS_EASE_")) continue;
        const value = declaration.initializer;
        const isConst = (statement.declarationList.flags & ts.NodeFlags.Const) !== 0;
        if (!isConst || !value || !ts.isStringLiteral(value) || !CSS_EASING_RE.test(value.text)) {
          issues.push(`${filename}:${source.getLineAndCharacterOfPosition(declaration.getStart()).line + 1}: shared ${declaration.name.text} must be a const CSS easing string`);
          continue;
        }
        motionEasingImports.add(declaration.name.text);
      }
    }
  }

  const line = (node) => source.getLineAndCharacterOfPosition(node.getStart()).line + 1;
  const fail = (node, message) => issues.push(`${filename}:${line(node)}: ${message}`);
  const visit = (node) => {
    if (ts.isStringLiteralLike(node) && LEGACY_EASING_RE.test(node.text)) {
      fail(node, `legacy GSAP easing token ${JSON.stringify(node.text.match(LEGACY_EASING_RE)?.[0])} cannot enter production source`);
    }
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === "animate"
    ) {
      const options = node.arguments[1];
      if (options && !ts.isNumericLiteral(options)) {
        if (!ts.isObjectLiteralExpression(options)) {
          fail(options, "Element.animate options must be an inline object so easing cannot be forwarded opaquely");
        } else {
          for (const property of options.properties) {
            if (ts.isSpreadAssignment(property)) {
              fail(property, "Element.animate options cannot spread opaque values");
            } else if (ts.isShorthandPropertyAssignment(property) && property.name.text === "easing") {
              fail(property, "Element.animate easing must be explicit, not shorthand");
            }
          }
          const easing = options.properties.find((property) =>
            ts.isPropertyAssignment(property) && propertyName(property.name) === "easing",
          );
          if (easing && ts.isPropertyAssignment(easing)) {
            const value = easing.initializer;
            if (ts.isStringLiteral(value)) {
              if (!CSS_EASING_RE.test(value.text)) fail(value, `${JSON.stringify(value.text)} is not an allowed CSS easing`);
            } else if (ts.isIdentifier(value)) {
              if (!motionEasingImports.has(value.text)) {
                fail(value, `easing identifier ${value.text} must be a CSS_EASE_* token imported from the shared motion module`);
              }
            } else {
              fail(value, "Element.animate easing must be a CSS literal or a shared CSS_EASE_* token");
            }
          }
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return issues;
}

function selfTest() {
  const cases = [
    ["valid shared token", 'import { CSS_EASE_OUT } from "./motion"; el.animate([], { duration: 1, easing: CSS_EASE_OUT });', 0],
    ["valid Windows shared declaration", 'export const CSS_EASE_IN = "ease-in"; el.animate([], { easing: CSS_EASE_IN });', 0, "src\\lib\\motion.ts"],
    ["valid CSS literal", 'el.animate([], { easing: "cubic-bezier(0.2, 0.72, 0.2, 1)" });', 0],
    ["legacy token", 'el.animate([], { easing: "power2.in" });', 2],
    ["opaque options", "el.animate([], options);", 1],
    ["spread options", "el.animate([], { duration: 1, ...legacyOptions });", 1],
    ["shorthand easing", "el.animate([], { easing });", 1],
    ["unowned identifier", "el.animate([], { easing: externalEase });", 1],
  ];
  for (const [name, source, expected, filename = `${name}.ts`] of cases) {
    const actual = validateSource(source, filename).length;
    if (actual !== expected) throw new Error(`${name}: expected ${expected} contract findings, got ${actual}`);
  }
  console.log(`waapi-contract: ${cases.length} self-tests passed`);
}

if (process.argv.includes("--self-test")) selfTest();

const issues = sourceFiles(SOURCE_ROOT).flatMap((path) => validateSource(readFileSync(path, "utf8"), relative(".", path)));
if (issues.length > 0) {
  console.error(`waapi-contract: ${issues.length} violation(s)`);
  for (const issue of issues) console.error(`  ${issue}`);
  process.exit(1);
}
console.log("waapi-contract: production Web Animations options use explicit CSS-compatible easing");
