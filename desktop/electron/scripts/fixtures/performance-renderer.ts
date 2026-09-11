import { installPerformancePressureMonitor } from "../../../frontend/src/lib/crash";

const params = new URLSearchParams(location.search);
if (params.get("benchmark") === "1") {
  document.hasFocus = () => true;
  Object.defineProperty(document, "visibilityState", { get: () => "visible" });
}
if (params.get("monitor") === "1") installPerformancePressureMonitor();

function burn(ms: number) {
  const end = performance.now() + ms;
  while (performance.now() < end) Math.sqrt(Math.random());
}
async function run(ms: number) {
  const times: number[] = [];
  let frames = 0;
  let last = performance.now();
  const start = last;
  await new Promise<void>((resolve) => {
    function workFrame() {
      const now = performance.now();
      times.push(now - last);
      last = now;
      let sum = 0;
      for (let i = 0; i < 100000; i++) sum += Math.sin(i + frames);
      document.getElementById("output")!.textContent = Array(200).fill(String(sum)).join("\n");
      frames++;
      if (now - start < ms) requestAnimationFrame(workFrame); else resolve();
    }
    requestAnimationFrame(workFrame);
  });
  times.sort((a, b) => a - b);
  return { frames, elapsedMs: performance.now() - start, frameP95Ms: times[Math.floor(times.length * .95)], frameMaxMs: times[times.length - 1] };
}
Object.assign(window, { diagnosticFixture: { run, burn } });
document.getElementById("work")!.onclick = () => burn(950);
