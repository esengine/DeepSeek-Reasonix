import { performance } from "node:perf_hooks";

const base = process.env.REASONIX_PERF_URL || "http://127.0.0.1:5173";
const rounds = Number(process.env.REASONIX_PERF_ROUNDS || "5");
const paths = [
  "/?platform=windows",
  "/?platform=windows&surface=plugins",
  "/?platform=windows&mock=thread",
  "/?platform=windows&mock=thread&theme=dark",
  "/?platform=windows&mock=thread&theme=light",
];

function percentile(values, p) {
  const sorted = [...values].sort((a, b) => a - b);
  const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil((p / 100) * sorted.length) - 1));
  return sorted[idx] ?? 0;
}

async function sample(path) {
  const url = new URL(path, base);
  const started = performance.now();
  const res = await fetch(url, { cache: "no-store" });
  await res.arrayBuffer();
  return { status: res.status, ms: performance.now() - started };
}

console.log(`Reasonix perf smoke: ${base} (${rounds} rounds)`);
for (const path of paths) {
  const values = [];
  let status = 0;
  for (let i = 0; i < rounds; i += 1) {
    const result = await sample(path);
    status = result.status;
    values.push(result.ms);
  }
  console.log(`${path.padEnd(42)} status=${status} p50=${percentile(values, 50).toFixed(1)}ms p95=${percentile(values, 95).toFixed(1)}ms`);
}
