// Runs in real Electron via scripts/window-geometry-smoke.mjs.
import assert from "node:assert/strict";
import type { EventEmitter } from "node:events";
import { join } from "node:path";
import { app, screen, type BrowserWindow } from "electron";
import { MainWindow } from "./window.js";
import { AppZoomStore } from "./zoomStore.js";
import { restoreWindowRect, type WindowRect } from "./windowBounds.js";
import type { HelloWindow } from "./handshake.js";

function near(actual: WindowRect, expected: WindowRect): void {
  for (const key of ["x", "y", "width", "height"] as const) {
    assert.ok(Math.abs(actual[key] - expected[key]) <= 1, `${key}: ${actual[key]} != ${expected[key]}`);
  }
}

async function transition(win: BrowserWindow, event: "maximize" | "minimize" | "restore" | "unmaximize", action: () => void): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`no ${event} event: ${JSON.stringify({ bounds: win.getBounds(), maximized: win.isMaximized(), minimized: win.isMinimized(), visible: win.isVisible() })}`)), 10_000);
    (win as EventEmitter).once(event, () => { clearTimeout(timer); resolve(); });
    action();
  });
}

async function run(): Promise<void> {
  await app.whenReady();
  const workArea = screen.getPrimaryDisplay().workArea;
  const geometry: HelloWindow = {
    width: Math.min(1000, workArea.width - 100), height: Math.min(650, workArea.height - 100),
    minWidth: 400, minHeight: 300, frameless: process.platform === "win32", zoomFactor: 1,
    position: { x: workArea.x + 40, y: workArea.y + 40 },
  };
  function create(input: HelloWindow): MainWindow {
    const main = new MainWindow({
      preloadPath: join(app.getPath("userData"), "unused-preload.cjs"),
      appURL: "https://example.invalid", platform: process.platform,
      log: console, zoomStore: new AppZoomStore(join(app.getPath("userData"), "zoom.json")),
      onAppDomReady() {}, onCloseRequested: async () => true, onCloseAllowed() {}, onShellAction() {},
    });
    main.create(input);
    return main;
  }
  const main = create(geometry);
  const win = main.browserWindow!;
  const expected = restoreWindowRect(geometry, geometry.position, workArea);
  near(win.getBounds(), expected);
  assert.equal(win.isVisible(), false, "restore geometry must be applied before show");
  await win.loadURL("about:blank");
  win.show();
  await transition(win, "maximize", () => main.maximise());
  // Do not poll bounds between maximise and minimise: native event capture
  // must retain the restore geometry even before the renderer's first save.
  await transition(win, "minimize", () => main.minimise());
  near(main.bounds(), expected);
  assert.equal(main.bounds().maximised, true, "minimise must preserve maximised intent");
  await transition(win, "restore", () => main.unminimise());
  await transition(win, "unmaximize", () => main.unmaximise());
  near(win.getBounds(), expected);
  assert.equal(main.bounds().maximised, false);
  await transition(win, "maximize", () => main.maximise());
  const saved = main.bounds();
  win.destroy();
  const relaunched = create({ ...geometry, width: saved.width, height: saved.height, position: { x: saved.x, y: saved.y } });
  near(relaunched.bounds(), expected);
  relaunched.browserWindow!.destroy();
  const legacy = create({ ...geometry, width: workArea.width + 16, height: workArea.height + 16,
    position: { x: workArea.x - 8, y: workArea.y - 8 } });
  near(legacy.browserWindow!.getBounds(), workArea);
  legacy.browserWindow!.destroy();
  console.log("PASS native geometry: custom restore, maximise/minimise, relaunch, legacy oversized frame");
}

// Avoid quitting between the individual window cases.
app.on("window-all-closed", () => {});
void run().then(() => app.exit(0), (error: unknown) => { console.error(error); app.exit(1); });
