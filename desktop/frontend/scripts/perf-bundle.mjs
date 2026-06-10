import { readdir, stat } from "node:fs/promises";
import path from "node:path";

const dist = path.resolve("dist");

async function walk(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true }).catch(() => [])) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...await walk(full));
    else out.push(full);
  }
  return out;
}

function bucket(file) {
  if (file.endsWith(".js")) return "js";
  if (file.endsWith(".css")) return "css";
  if (/\.(woff2?|ttf|otf)$/i.test(file)) return "font";
  return "asset";
}

function kb(bytes) {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

const files = await walk(dist);
if (files.length === 0) {
  console.error("dist is empty. Run npm run build before npm run perf:bundle.");
  process.exit(1);
}

const rows = [];
for (const file of files) {
  const info = await stat(file);
  rows.push({ file: path.relative(dist, file).replace(/\\/g, "/"), bytes: info.size, kind: bucket(file) });
}

const totals = new Map();
for (const row of rows) totals.set(row.kind, (totals.get(row.kind) ?? 0) + row.bytes);

console.log("Bundle size by type");
for (const [kind, bytes] of [...totals.entries()].sort()) console.log(`${kind.padEnd(6)} ${kb(bytes)}`);
console.log("\nLargest files");
for (const row of rows.sort((a, b) => b.bytes - a.bytes).slice(0, 12)) {
  console.log(`${kb(row.bytes).padStart(10)}  ${row.file}`);
}
