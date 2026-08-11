const { app } = require("electron");
const http = require("node:http");
const { CompanionApp } = require("../dist/app.js");

const startedAt = Number(process.env.REASONIX_PERF_STARTED_AT || Date.now());

app.whenReady().then(async () => {
  let server;
  try {
    server = http.createServer((_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end("<html><head><title>Perf</title></head><body><button>ok</button></body></html>");
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const url = `http://127.0.0.1:${server.address().port}/`;
    const companion = new CompanionApp({
      componentVersion: "perf",
      electronVersion: process.versions.electron,
      chromiumVersion: process.versions.chrome,
      pid: process.pid,
      emitEvent: () => {},
      exit: () => {},
    });
    companion.markReady();
    await companion.handle(request("hello", "hello", "", { hostName: "perf", hostVersion: "perf" }));
    await waitForLoad(companion.chrome.view.webContents);
    const startupMs = Date.now() - startedAt;

    await delay(1200);
    app.getAppMetrics();
    await delay(1000);
    const idleCPUPercent = cpuPercent();

    const first = await companion.handle(request("one", "tab.open", "perf", {
      ownerId: "perf", url, disposition: "foreground",
    }));
    await companion.handle(request("wait-one", "tab.wait", "perf", {
      ownerId: "perf", tabId: first.result.tabId, waitUntil: "load", timeoutMs: 8000,
    }));
    await delay(300);
    const oneTabRSSMiB = rssMiB();

    for (let i = 1; i < 8; i += 1) {
      const opened = await companion.handle(request(`tab-${i}`, "tab.open", "perf", {
        ownerId: "perf", url: `${url}?tab=${i}`, disposition: "background",
      }));
      await companion.handle(request(`wait-${i}`, "tab.wait", "perf", {
        ownerId: "perf", tabId: opened.result.tabId, waitUntil: "load", timeoutMs: 8000,
      }));
    }
    await delay(500);
    const eightTabRSSMiB = rssMiB();
    console.error("PERF_RESULT: " + JSON.stringify({
      startupMs,
      idleCPUPercent: round(idleCPUPercent),
      oneTabRSSMiB: round(oneTabRSSMiB),
      eightTabRSSMiB: round(eightTabRSSMiB),
      incrementalRSSMiB: round(eightTabRSSMiB - oneTabRSSMiB),
      liveTabs: companion.tabs.liveCount,
    }));
  } catch (err) {
    console.error("PERF_FATAL: " + (err && err.stack ? err.stack : err));
  } finally {
    server?.close();
  }
});

function request(requestId, method, ownerId, params) {
  return { protocolVersion: 1, requestId, ownerId, method, params };
}

function waitForLoad(wc) {
  if (!wc.isLoading()) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("chrome load timeout")), 8000);
    wc.once("did-finish-load", () => { clearTimeout(timer); resolve(); });
  });
}

function delay(ms) { return new Promise((resolve) => setTimeout(resolve, ms)); }
function rssMiB() { return app.getAppMetrics().reduce((sum, metric) => sum + (metric.memory?.workingSetSize || 0), 0) / 1024; }
function cpuPercent() { return app.getAppMetrics().reduce((sum, metric) => sum + (metric.cpu?.percentCPU || 0), 0); }
function round(n) { return Math.round(n * 10) / 10; }

setTimeout(() => console.error("PERF_FATAL: probe timeout"), 30000);
