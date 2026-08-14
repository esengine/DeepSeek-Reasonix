import { access, readFile } from "node:fs/promises";
import path from "node:path";

const LABELS = {
  mineru: /^mineru(?:[\s_-]+api)?(?:[\s_-]+(?:key|token))?$/i,
  deepseek: /^deepseek(?:[\s_-]+api)?(?:[\s_-]+(?:key|token))?$/i,
};

export function parseCredentialFile(content) {
  const lines = String(content).split(/\r?\n/).map((line) => line.trim());
  const credentials = {};
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (!line) continue;
    for (const [name, pattern] of Object.entries(LABELS)) {
      const split = line.match(/^([^:=]+)(?:[:=]\s*(.+))?$/);
      const label = split?.[1]?.trim() ?? "";
      if (!pattern.test(label)) continue;
      const inline = split?.[2]?.trim();
      const next = lines.slice(index + 1).find((candidate) => candidate.length > 0);
      const value = inline || next;
      if (value) credentials[name] = value;
    }
  }
  return credentials;
}

async function firstReadable(paths) {
  for (const candidate of paths) {
    try {
      await access(candidate);
      return candidate;
    } catch {
      // Keep searching without disclosing path or credential content.
    }
  }
  return null;
}

export async function loadRuntimeConfig(options = {}) {
  const env = options.env ?? process.env;
  const cwd = options.cwd ?? process.cwd();
  const candidates = [
    options.keyFile,
    env.INTELIFAR_API_KEY_FILE,
    path.resolve(cwd, "apikey.txt"),
    path.resolve(cwd, "..", "apikey.txt"),
    path.resolve(cwd, "..", "..", "apikey.txt"),
  ].filter(Boolean);
  const keyFile = await firstReadable(candidates);
  const fileCredentials = keyFile ? parseCredentialFile(await readFile(keyFile, "utf8")) : {};
  const mineruApiKey = env.MINERU_API_KEY?.trim() || fileCredentials.mineru;
  const deepseekApiKey = env.DEEPSEEK_API_KEY?.trim() || fileCredentials.deepseek;
  const missing = [!mineruApiKey && "MinerU", !deepseekApiKey && "DeepSeek"].filter(Boolean);
  if (missing.length) throw new Error(`Missing runtime credentials: ${missing.join(", ")}`);
  return {
    mineruApiKey,
    deepseekApiKey,
    deepseekModel: env.DEEPSEEK_MODEL?.trim() || "deepseek-v4-flash",
    httpsProxy: env.INTELIFAR_HTTPS_PROXY?.trim() || env.HTTPS_PROXY?.trim() || env.HTTP_PROXY?.trim() || (process.platform === "win32" ? "http://127.0.0.1:10809" : null),
    keyFile,
  };
}
