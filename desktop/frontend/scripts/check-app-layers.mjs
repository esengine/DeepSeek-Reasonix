#!/usr/bin/env node
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { basename, dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import ts from "typescript";

const common = new Set(["useCommittedSlot.ts", "useCommittedCommand.ts", "useCommittedAsyncCommand.ts", "commandOutcome.ts", "composeDomRef.ts", "subscriptionScope.ts"]);
const domNames = new Set(["window", "document", "HTMLElement", "HTMLDivElement", "ReactNode", "SyntheticEvent"]);

function sourceFiles(root) {
  if (!existsSync(root)) return [];
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory() ? sourceFiles(join(root, entry.name))
      : /\.[cm]?[jt]sx?$/.test(entry.name) ? [join(root, entry.name)] : []);
}

export function moduleEdges(code, file) {
  const tree = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true);
  const edges = [];
  const identifiers = new Set();
  const namedTypesOnly = (bindings) => bindings && ts.isNamedImports(bindings)
    && bindings.elements.length > 0 && bindings.elements.every((entry) => entry.isTypeOnly);
  function visit(node) {
    if (ts.isIdentifier(node)) identifiers.add(node.text);
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const clause = node.importClause;
      edges.push({ specifier: node.moduleSpecifier.text,
        typeOnly: Boolean(clause?.isTypeOnly || (clause && !clause.name && namedTypesOnly(clause.namedBindings))) });
      return;
    } else if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      edges.push({ specifier: node.moduleSpecifier.text, typeOnly: Boolean(node.isTypeOnly
        || (node.exportClause && ts.isNamedExports(node.exportClause) && node.exportClause.elements.length > 0
          && node.exportClause.elements.every((entry) => entry.isTypeOnly))) });
      return;
    } else if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword
      || (ts.isIdentifier(node.expression) && node.expression.text === "require"))) {
      const argument = node.arguments[0];
      if (argument && ts.isStringLiteral(argument)) edges.push({ specifier: argument.text, typeOnly: false });
      else edges.push({ specifier: "<non-literal module>", typeOnly: false, unresolved: true });
    } else if (ts.isImportEqualsDeclaration(node) && ts.isExternalModuleReference(node.moduleReference)
      && node.moduleReference.expression && ts.isStringLiteral(node.moduleReference.expression)) {
      edges.push({ specifier: node.moduleReference.expression.text, typeOnly: Boolean(node.isTypeOnly) });
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  return { edges, identifiers };
}

export function checkAppLayers(sourceRoot, compilerOptions = {}) {
  const failures = new Set();
  const cache = new Map();
  const normalize = (file) => relative(sourceRoot, file).replaceAll("\\", "/");
  const parse = (file) => {
    if (!cache.has(file)) cache.set(file, moduleEdges(readFileSync(file, "utf8"), file));
    return cache.get(file);
  };
  const resolved = (edge, from) => {
    if (edge.unresolved) return null;
    const result = ts.resolveModuleName(edge.specifier, from, compilerOptions, ts.sys).resolvedModule;
    return result && !result.isExternalLibraryImport ? result.resolvedFileName : null;
  };
  const files = ["app-shell", "app-runtime", "app-features", "app-domain"]
    .flatMap((directory) => sourceFiles(join(sourceRoot, directory)));
  for (const name of common) {
    const file = join(sourceRoot, "lib", name);
    if (existsSync(file)) files.push(file);
  }
  for (const file of files) {
    const name = normalize(file);
    const shell = name.startsWith("app-shell/");
    const domain = /Owner\.ts$/.test(basename(file)) || basename(file) === "sessionTarget.ts" || name.startsWith("app-domain/");
    const foundation = common.has(basename(file));
    const visited = new Set();
    function inspect(current, chain) {
      if (visited.has(current)) return;
      visited.add(current);
      const parsed = parse(current);
      if (domain && [...parsed.identifiers].some((id) => domNames.has(id))) {
        failures.add(name + ": domain reaches DOM/React objects through " + chain.join(" -> "));
      }
      for (const edge of parsed.edges) {
        if (edge.typeOnly) continue;
        if (/\.(?:css|svg|png|webp|woff2?)(?:\?.*)?$/.test(edge.specifier)
          && existsSync(resolve(dirname(current), edge.specifier.split("?")[0]))) continue;
        const target = resolved(edge, current);
        const targetName = target ? normalize(target) : edge.specifier;
        const next = [...chain, targetName];
        if (edge.unresolved || (!target && edge.specifier.startsWith("."))) {
          failures.add(name + ": unresolvable runtime dependency " + next.join(" -> "));
        }
        if (domain && (/^react(?:-dom)?(?:\/|$)/.test(edge.specifier)
          || /^(?:app-shell|components)\//.test(targetName))) {
          failures.add(name + ": domain reaches presentation through " + next.join(" -> "));
        }
        if (!shell && targetName.startsWith("app-shell/")) {
          failures.add(name + ": upstream reaches presentation through " + next.join(" -> "));
        }
        if (foundation && /^app-(?:runtime|features|shell)\//.test(targetName)) {
          failures.add(name + ": shared primitive reaches App through " + next.join(" -> "));
        }
        if (shell && targetName === "lib/bridge.ts") {
          failures.add(name + ": presentation reaches bridge through " + next.join(" -> "));
        }
        // Existing leaf components retain their own contracts; follow shell-local
        // wrappers and the complete runtime graph of domain/common modules.
        if (target && (domain || foundation || (shell && targetName.startsWith("app-shell/")))) inspect(target, next);
      }
    }
    inspect(file, [name]);
  }
  return [...failures];
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const frontend = dirname(dirname(fileURLToPath(import.meta.url)));
  const config = ts.readConfigFile(join(frontend, "tsconfig.json"), ts.sys.readFile);
  if (config.error) throw new Error(ts.flattenDiagnosticMessageText(config.error.messageText, "\n"));
  const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, frontend);
  const failures = checkAppLayers(join(frontend, "src"), parsed.options);
  for (const failure of failures) console.error("check-app-layers: " + failure);
  if (failures.length) process.exitCode = 1;
  else console.log("check-app-layers: migrated App modules satisfy the AST dependency contracts");
}
