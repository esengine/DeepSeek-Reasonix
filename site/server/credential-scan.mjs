import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadRuntimeConfig } from "./config.mjs";

const TEXT_EXTENSIONS = new Set([".astro", ".css", ".csv", ".html", ".js", ".json", ".md", ".mjs", ".toml", ".txt", ".yaml", ".yml"]);
const EXCLUDED = new Set([".git", ".astro", "node_modules", "dist"]);

export async function scanCredentialLeaks({ root, secrets }) {
  const hits = [];
  async function visit(directory) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      if (EXCLUDED.has(entry.name)) continue;
      const target = path.join(directory, entry.name);
      if (entry.isDirectory()) await visit(target);
      else if (entry.isFile() && TEXT_EXTENSIONS.has(path.extname(entry.name).toLowerCase())) {
        const content = await readFile(target, "utf8");
        if (secrets.some((secret) => secret && content.includes(secret))) hits.push(path.relative(root, target));
      }
    }
  }
  await visit(root);
  return hits;
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const siteRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const repoRoot = path.resolve(siteRoot, "..");
  const config = await loadRuntimeConfig({ cwd: siteRoot });
  const hits = await scanCredentialLeaks({ root: repoRoot, secrets: [config.mineruApiKey, config.deepseekApiKey] });
  if (hits.length) {
    process.stderr.write(`Credential leak scan failed in ${hits.length} file(s):\n${hits.join("\n")}\n`);
    process.exitCode = 1;
  } else {
    process.stdout.write("Credential leak scan passed: no runtime key values found in project text artifacts.\n");
  }
}
