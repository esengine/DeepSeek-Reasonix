import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import ts from "typescript";
import { checkAppLayers, moduleEdges } from "./check-app-layers.mjs";

const fixture = mkdtempSync(join(tmpdir(), "reasonix-app-layers-"));
const options = { moduleResolution: ts.ModuleResolutionKind.Bundler, baseUrl: fixture, paths: { "@/*": ["*"] } };
const write = (name, source) => {
  const file = join(fixture, name);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, source);
};
try {
  const parsed = moduleEdges(`
    // import React from 'react';
    import type { ReactNode } from 'react';
    import { type Config } from './types';
    export { type Target } from './types';
    export * from './runtime';
    const later = () => import('./lazy');
  `, "fixture.ts");
  assert.deepEqual(parsed.edges.map((edge) => [edge.specifier, edge.typeOnly]), [
    ["react", true], ["./types", true], ["./types", true], ["./runtime", false], ["./lazy", false],
  ]);
  write("app-domain/owner.ts", "export { run } from '@/lib/middle';");
  write("lib/middle.ts", "export const run = () => import('./leaf');");
  write("lib/leaf.ts", "import React from 'react'; export const value = React;");
  assert.ok(checkAppLayers(fixture, options).some((failure) => failure.includes("domain reaches presentation")),
    "alias, re-export and lazy edges cannot conceal a transitive React dependency");
  write("lib/leaf.ts", "export const value = document.title;");
  assert.ok(checkAppLayers(fixture, options).some((failure) => failure.includes("domain reaches DOM")));
  write("lib/leaf.ts", "import type { ReactNode } from 'react'; export const value = 1;");
  assert.deepEqual(checkAppLayers(fixture, options), []);
  write("lib/leaf.ts", "export const load = (name: string) => import(name);");
  assert.ok(checkAppLayers(fixture, options).some((failure) => failure.includes("unresolvable runtime dependency")));
  write("lib/leaf.ts", "export const value = 1;");
  write("app-shell/Region.tsx", "export { run } from './wrapper';");
  write("app-shell/wrapper.ts", "export { app as run } from '@/lib/bridge';");
  write("lib/bridge.ts", "export const app = {};");
  assert.ok(checkAppLayers(fixture, options).some((failure) => failure.includes("presentation reaches bridge")));
  write("app-shell/wrapper.ts", "export const run = 1;");
  write("lib/useCommittedSlot.ts", "export * from '../app-runtime/adapter';");
  write("app-runtime/adapter.ts", "export const value = 1;");
  assert.ok(checkAppLayers(fixture, options).some((failure) => failure.includes("shared primitive reaches App")));
  console.log("PASS AST layer checks resolve runtime edges and reject transitive boundary violations");
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
