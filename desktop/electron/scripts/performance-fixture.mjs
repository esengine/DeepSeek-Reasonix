import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createRequire } from "node:module";
import { build } from "esbuild";
import { _electron } from "playwright";

const root = resolve(import.meta.dirname, "..");

// The fixture uses production protocol, preload, diagnostic owner and Worker.
// Mode and paths remain JSON data, never interpolated executable source.
export async function performanceFixture(monitor = true, { archiveWorker = false, benchmark = false } = {}) {
  const temp = mkdtempSync(join(tmpdir(), "reasonix-perf-"));
  let app;
  try {
    mkdirSync(join(temp, "assets"));
    writeFileSync(join(temp, "index.html"), '<!doctype html><title>Reasonix diagnostic fixture</title><button id="work">Work</button><pre id="output"></pre><script src="/assets/workload.js"></script>');
    await build({
      bundle: true, platform: "browser", format: "iife", target: "chrome130",
      entryPoints: [join(import.meta.dirname, "fixtures/performance-renderer.ts")],
      outfile: join(temp, "assets/workload.js"),
      define: { __BUILD_COMMIT__: '"diagnostic-fixture"', __BUILD_CHANNEL__: '"dev"', "import.meta.env.DEV": "false", "import.meta.env.MODE": '"production"' },
    });
    const common = { bundle: true, platform: "node", format: "cjs", external: ["electron"] };
    await build({ ...common, entryPoints: [join(root, "src/preload/index.ts")], outfile: join(temp, "preload.cjs") });
    await build({ ...common, entryPoints: [join(root, "src/main/profileAnalysisWorker.ts")], outfile: join(temp, "profile-analysis.cjs") });
    let workerPath = join(temp, "profile-analysis.cjs");
    if (archiveWorker) {
      const desktopRequire = createRequire(join(root, "../package.json"));
      const packagerRequire = createRequire(desktopRequire.resolve("@electron/packager"));
      const { createPackage } = packagerRequire("@electron/asar");
      const bundle = join(temp, "worker-bundle");
      mkdirSync(bundle);
      await build({ ...common, entryPoints: [join(root, "src/main/profileAnalysisWorker.ts")], outfile: join(bundle, "profile-analysis.cjs") });
      await createPackage(bundle, join(temp, "worker.asar"));
      workerPath = join(temp, "worker.asar/profile-analysis.cjs");
    }
    writeFileSync(join(temp, "performance-config.json"), JSON.stringify({ monitor, benchmark, workerPath }));
    await build({ ...common, entryPoints: [join(import.meta.dirname, "fixtures/performance-main.ts")], outfile: join(temp, "main.cjs") });
    app = await _electron.launch({ args: [join(temp, "main.cjs")] });
    const page = await app.firstWindow();
    await page.waitForFunction(() => Boolean(window.diagnosticFixture && window.reasonixDesktop));
    if (!benchmark) await page.bringToFront();
    return { app, page, temp, async close() { await app.close(); rmSync(temp, { recursive: true, force: true }); } };
  } catch (error) {
    await app?.close();
    rmSync(temp, { recursive: true, force: true });
    throw error;
  }
}
