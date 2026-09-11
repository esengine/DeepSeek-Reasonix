import { app, BrowserWindow, protocol, ipcMain, net } from "electron";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { registerAppProtocol } from "../../src/main/protocol.js";
import { registerRendererIpc } from "../../src/main/ipc.js";
import { ProcessDiagnostics } from "../../src/main/processDiagnostics.js";
import { createPerformanceHost } from "../../src/main/performanceHost.js";

const config = JSON.parse(readFileSync(join(__dirname, "performance-config.json"), "utf8")) as {
  benchmark: boolean; monitor: boolean; workerPath: string;
};
app.setPath("userData", join(__dirname, "profile"));
protocol.registerSchemesAsPrivileged([{ scheme: "reasonix", privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: false, stream: true } }]);
app.whenReady().then(async () => {
  const log = { info() {}, warn() {}, error() {} };
  let copied = "";
  const clipboard = { writeText: (text: string) => { copied = text; }, readText: () => copied };
  registerAppProtocol({ protocol, fetch: net.fetch, distRoot: __dirname, resources: () => null, log });
  const win = new BrowserWindow({
    show: true, focusable: !config.benchmark, width: 1000, height: 750,
    webPreferences: { backgroundThrottling: !config.benchmark, preload: join(__dirname, "preload.cjs"), sandbox: true, contextIsolation: true, nodeIntegration: false },
  });
  const diagnostics = new ProcessDiagnostics(() => app.getAppMetrics(), undefined, () => config.benchmark || (win.isVisible() && win.isFocused()));
  if (config.monitor) { diagnostics.sample(); setInterval(() => diagnostics.sample(), 30000).unref(); }
  const performanceHost = createPerformanceHost({
    window: () => win.isDestroyed() ? null : win,
    workerPath: config.workerPath,
    locale: () => "en",
    isForeground: config.benchmark ? () => true : undefined,
    dialog: {
      showMessageBox: async () => ({ response: 1, checkboxChecked: false }),
      showSaveDialog: async () => ({ canceled: false, filePath: join(__dirname, "fixture.heapsnapshot") }),
    },
  });
  registerRendererIpc({
    ipcMain, contract: { protocolVersion: 1, digest: "test", commands: [], commandSet: new Set() },
    window: { isTrustedSender: (sender, frame) => sender === win.webContents && frame === sender.mainFrame } as Parameters<typeof registerRendererIpc>[0]["window"],
    clipboard, invoke: async () => undefined, openExternal: async () => undefined,
    serviceState: () => ({ phase: "ready", generation: "test" }),
    processDiagnostics: () => diagnostics.snapshot(), performance: performanceHost, log,
  });
  app.once("will-quit", () => performanceHost.dispose());
  const url = new URL("reasonix://app/index.html");
  url.searchParams.set("monitor", config.monitor ? "1" : "0");
  url.searchParams.set("benchmark", config.benchmark ? "1" : "0");
  await win.loadURL(url.href);
  if (!config.benchmark) win.focus();
});
