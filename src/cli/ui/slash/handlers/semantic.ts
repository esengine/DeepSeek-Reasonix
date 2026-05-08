/** Informational only — actual install/build runs via `reasonix index` to avoid suspending Ink. */

import { promises as fs } from "node:fs";
import path from "node:path";
import type { EmbeddingProvider } from "@/config.js";
import { t as tMain } from "@/i18n/index.js";
import { probeOllama } from "@/index/semantic/embedding.js";
import { t } from "@/index/semantic/i18n.js";
import { findOllamaBinary } from "@/index/semantic/ollama-launcher.js";
import type { SlashHandler } from "../dispatch.js";

const semantic: SlashHandler = (_args, _loop, ctx) => {
  const root = ctx.codeRoot;
  if (!root) {
    return { info: tMain("handlers.semantic.codeOnly") };
  }
  void (async () => {
    const status = await renderSemanticStatus(root);
    ctx.postInfo?.(status);
  })();
  return { info: tMain("handlers.semantic.checking") };
};

export async function renderSemanticStatus(rootDir: string): Promise<string> {
  const lines: string[] = [t("slashHeader"), ""];
  const indexMeta = await readIndexMeta(rootDir);
  if (indexMeta) {
    lines.push(t("slashEnabled"));
    lines.push(
      `${t("slashEnabledDetail", {
        chunks: indexMeta.chunks,
        files: indexMeta.files,
      })} · ${indexMeta.provider} · ${indexMeta.model}`,
    );
    lines.push(t("slashEnabledHowto"));
    return lines.join("\n");
  }
  lines.push(t("slashIndexMissing"));
  lines.push(t("slashIndexInfo"));
  lines.push("");
  if (findOllamaBinary() === null) {
    lines.push(t("slashOllamaMissing"));
  } else {
    const probe = await probeOllama();
    if (!probe.ok) lines.push(t("slashDaemonDown"));
  }
  lines.push(t("slashHowToBuild"));
  return lines.join("\n");
}

interface IndexSummary {
  provider: EmbeddingProvider;
  model: string;
  chunks: number;
  files: number;
}

async function readIndexMeta(rootDir: string): Promise<IndexSummary | null> {
  const metaPath = path.join(rootDir, ".reasonix", "semantic", "index.meta.json");
  const dataPath = path.join(rootDir, ".reasonix", "semantic", "index.jsonl");
  let meta: { provider?: EmbeddingProvider; model?: string };
  let raw: string;
  try {
    meta = JSON.parse(await fs.readFile(metaPath, "utf8"));
    const fh = await fs.open(dataPath, "r");
    try {
      const stat = await fh.stat();
      if (stat.size > 10 * 1024 * 1024) {
        return {
          provider: meta.provider === "openai-compat" ? "openai-compat" : "ollama",
          model: typeof meta.model === "string" ? meta.model : "",
          chunks: Math.round(stat.size / 500),
          files: 0,
        };
      }
      raw = await fh.readFile("utf8");
    } finally {
      await fh.close();
    }
    const seenPaths = new Set<string>();
    let chunks = 0;
    for (const line of raw.split("\n")) {
      if (line.length === 0) continue;
      chunks++;
      try {
        const parsed = JSON.parse(line) as { p?: string };
        if (parsed.p) seenPaths.add(parsed.p);
      } catch {
        /* tolerated */
      }
    }
    return {
      provider: meta.provider === "openai-compat" ? "openai-compat" : "ollama",
      model: typeof meta.model === "string" ? meta.model : "",
      chunks,
      files: seenPaths.size,
    };
  } catch {
    return null;
  }
}

export const handlers: Record<string, SlashHandler> = {
  semantic,
};
