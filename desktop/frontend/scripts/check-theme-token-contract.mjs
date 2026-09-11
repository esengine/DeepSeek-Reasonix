#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const sourceRoot = path.join(frontendRoot, "src");

const retiredTokens = new Set([
  "--fg-muted",
  "--bg-elev-1",
  "--hover",
  "--border-strong",
  "--shadow",
]);

// These properties are deliberately written by React/runtime geometry code.
// Preview tokens are isolated on ThemePreviewSurface and never form part of
// the application theme contract.
const runtimeTokens = new Set([
  "--composer-height",
  "--invocation-color",
  "--sidebar-expanded-width",
  "--transcript-row-estimate",
]);
const runtimePrefixes = ["--tp-"];

const requiredRootTokens = new Map([
  ["--stage", "var(--bg)"],
  ["--surface", "var(--bg-elev)"],
  ["--surface-2", "var(--bg-elev-2)"],
  ["--surface-3", "var(--bg-soft)"],
  ["--panel", "var(--bg-elev)"],
  ["--border-2", "color-mix(in srgb, var(--fg) 20%, transparent)"],
  ["--text", "var(--fg)"],
  ["--text-2", "var(--fg-dim)"],
  ["--text-3", "var(--fg-faint)"],
  ["--shadow-color", "#000"],
  ["--overlay-surface-bg", "var(--bg-elev)"],
]);

const themeStyles = ["graphite", "aurora", "slate", "carbon", "nocturne", "amber"];
const amberExpected = {
  dark: {
    "--bg": "#090a0c",
    "--bg-soft": "#111319",
    "--panel": "#191b22",
    "--sidebar-bg": "#0c0e12",
    "--fg": "#f4f5f7",
    "--fg-dim": "#c0c4cc",
    "--accent": "#d4632f",
    "--border": "#343945",
  },
  light: {
    "--bg": "#f7f8fb",
    "--bg-soft": "#eef2f7",
    "--panel": "#ffffff",
    "--sidebar-bg": "#f3f6fa",
    "--fg": "#111827",
    "--fg-dim": "#4b5563",
    "--accent": "#dd5b28",
    "--border": "#d8dee8",
  },
};

function listCSSFiles(directory) {
  const files = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...listCSSFiles(target));
    else if (entry.isFile() && target.endsWith(".css")) files.push(target);
  }
  return files.sort();
}

function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, " "));
}

function lineNumber(source, index) {
  return source.slice(0, index).split("\n").length;
}

function findBlock(source, selector) {
  const start = source.indexOf(selector);
  if (start < 0) return null;
  const open = source.indexOf("{", start + selector.length - 1);
  if (open < 0) return null;
  let depth = 0;
  for (let index = open; index < source.length; index += 1) {
    if (source[index] === "{") depth += 1;
    else if (source[index] === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(open + 1, index);
    }
  }
  return null;
}

function declarations(block) {
  const result = new Map();
  if (!block) return result;
  for (const match of block.matchAll(/(--[a-zA-Z0-9_-]+)\s*:\s*([^;]+);/g)) {
    result.set(match[1], match[2].trim());
  }
  return result;
}

function normalize(value) {
  return value.replace(/\s+/g, " ").trim().toLowerCase();
}

function resolveToken(name, tokens, seen = new Set()) {
  if (seen.has(name)) return null;
  const value = tokens.get(name);
  if (!value) return null;
  const alias = value.match(/^var\(\s*(--[a-zA-Z0-9_-]+)\s*\)$/);
  if (!alias) return normalize(value);
  const nextSeen = new Set(seen);
  nextSeen.add(name);
  return resolveToken(alias[1], tokens, nextSeen);
}

const cssFiles = listCSSFiles(sourceRoot);
const sources = new Map(cssFiles.map((file) => [file, stripComments(fs.readFileSync(file, "utf8"))]));
const definitions = new Set();
const errors = [];

for (const source of sources.values()) {
  for (const match of source.matchAll(/^\s*(--[a-zA-Z0-9_-]+)\s*:/gm)) definitions.add(match[1]);
  for (const match of source.matchAll(/(?<=[{;])\s*(--[a-zA-Z0-9_-]+)\s*:/g)) definitions.add(match[1]);
}

for (const [file, source] of sources) {
  for (const match of source.matchAll(/var\(\s*(--[a-zA-Z0-9_-]+)\s*(?=,|\))/g)) {
    const token = match[1];
    const next = source[match.index + match[0].length];
    const hasFallback = next === ",";
    const runtimeOwned = runtimeTokens.has(token) || runtimePrefixes.some((prefix) => token.startsWith(prefix));
    if (retiredTokens.has(token)) {
      errors.push(`${path.relative(frontendRoot, file)}:${lineNumber(source, match.index)} references retired ${token}`);
    } else if (!definitions.has(token) && !hasFallback && !runtimeOwned) {
      errors.push(`${path.relative(frontendRoot, file)}:${lineNumber(source, match.index)} references undefined ${token}`);
    }
  }
}

const mainStyles = sources.get(path.join(sourceRoot, "styles.css"));
if (!mainStyles) {
  errors.push("src/styles.css is missing");
} else {
  const rootTokens = declarations(findBlock(mainStyles, ":root {"));
  for (const [token, expected] of requiredRootTokens) {
    const actual = rootTokens.get(token);
    if (!actual) errors.push(`src/styles.css base :root is missing ${token}`);
    else if (normalize(actual) !== normalize(expected)) {
      errors.push(`src/styles.css base :root must define ${token}: ${expected}; (found ${actual})`);
    }
  }
  for (const style of themeStyles) {
    const darkSelector = `:root[data-theme-style="${style}"]`;
    const lightSelector = `:root[data-theme="light"][data-theme-style="${style}"]`;
    const autoLightSelector = `:root[data-theme-style="${style}"]:not([data-theme])`;
    if (!mainStyles.includes(`${darkSelector} {`)) errors.push(`src/styles.css is missing ${style} dark theme selector`);
    if (!mainStyles.includes(`${lightSelector} {`)) errors.push(`src/styles.css is missing ${style} forced-light selector`);
    if (!mainStyles.includes(`${autoLightSelector} {`)) errors.push(`src/styles.css is missing ${style} auto-light selector`);
  }

  const workbenchRefresh = mainStyles.slice(mainStyles.indexOf("* Native Workbench refresh"));
  if (/^:root\s*\{/m.test(workbenchRefresh)) {
    errors.push("Native Workbench refresh must not override named dark theme palettes");
  }
  if (/^:root\[data-theme="light"\]\s*\{/m.test(workbenchRefresh)) {
    errors.push("Native Workbench refresh must not override named forced-light theme palettes");
  }
  if (/^\s*:root:not\(\[data-theme\]\)\s*\{/m.test(workbenchRefresh)) {
    errors.push("Native Workbench refresh must not override named auto-light theme palettes");
  }

  const darkAmber = new Map([
    ...rootTokens,
    ...declarations(findBlock(mainStyles, ':root[data-theme-style="amber"] {')),
  ]);
  const lightAmber = new Map([
    ...rootTokens,
    ...declarations(findBlock(mainStyles, ':root[data-theme="light"] {')),
    ...declarations(findBlock(mainStyles, ':root[data-theme="light"][data-theme-style="amber"] {')),
  ]);
  for (const [mode, tokens] of [["dark", darkAmber], ["light", lightAmber]]) {
    for (const [token, expected] of Object.entries(amberExpected[mode])) {
      const actual = resolveToken(token, tokens);
      if (actual !== normalize(expected)) {
        errors.push(`Amber ${mode} ${token} must resolve to ${expected} (found ${actual ?? "undefined"})`);
      }
    }
  }
}

if (errors.length > 0) {
  console.error("Theme token contract check failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

console.log(`Theme token contract OK (${cssFiles.length} CSS files, ${definitions.size} defined tokens).`);
