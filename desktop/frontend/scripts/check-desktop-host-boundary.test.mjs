import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { checkDesktopHostBoundary, desktopHostViolations } from "./check-desktop-host-boundary.mjs";

const fixture = mkdtempSync(join(tmpdir(), "reasonix-host-boundary-"));
const write = (name, source) => {
  const file = join(fixture, name);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, source);
};
try {
  const flagged = desktopHostViolations(`
    const a = window.go?.main?.App;
    const b = window.runtime?.EventsOn("x", () => {});
    const c = (window as unknown as { runtime?: unknown }).runtime;
    const d = globalThis.window.runtime;
    const e = window["go"];
    const f = window.reasonixDesktop?.browser;
    import { EventsOn } from "../../wailsjs/runtime/runtime";
    export { X } from "@wailsapp/runtime";
    const lazy = () => import("../wailsjs/go/main/App");
  `, "flagged.ts");
  assert.deepEqual(flagged.map((entry) => entry.replace(/^\d+: /, "")), [
    "window.go", "window.runtime", "window.runtime", "window.runtime", 'window["go"]', "window.reasonixDesktop",
    "import from ../../wailsjs/runtime/runtime", "export from @wailsapp/runtime", "dynamic import of ../wailsjs/go/main/App",
  ]);

  const clean = desktopHostViolations(`
    // window.go and window.runtime are only mentioned in this comment.
    const paths = ["frontend/wailsjs/runtime/runtime.js", "window.runtime"];
    const wails = window.wails;
    const phase = tab.runtime.phase;
    const goCount = stats.go;
    const host = shell.reasonixDesktop;
  `, "clean.ts");
  assert.deepEqual(clean, [], "comments, strings and unrelated members are not host access");

  const typeOnly = desktopHostViolations(`
    import type * as GeneratedApp from "../../wailsjs/go/main/App";
  `, "type-only.ts");
  assert.deepEqual(typeOnly.map((entry) => entry.replace(/^\d+: /, "")), ["import from ../../wailsjs/go/main/App"],
    "retired shell modules are rejected even when imported type-only");

  write("lib/desktopHost.ts", "export const host = window.go?.main?.App && window.runtime;");
  write("__tests__/x.test.ts", "window.runtime = {};");
  write("components/Thing.tsx", "export const v = window.runtime;");
  write("app-runtime/ok.ts", "export const v = 1;");
  assert.deepEqual(checkDesktopHostBoundary(fixture), ["components/Thing.tsx:1: window.runtime"],
    "only lib/desktopHost.ts and tests may reach the shell globals");
  console.log("PASS desktop host boundary gate flags shell-global access outside lib/desktopHost.ts");
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
