/** Informational only — actual install/build runs via `reasonix index` to avoid suspending Ink. */

import { promises as fs } from "node:fs";
import path from "node:path";
import { probeOllama } from "@/index/semantic/embedding.js";
import { t } from "@/index/semantic/i18n.js";
import { findOllamaBinary } from "@/index/semantic/ollama-launcher.js";
import type { SlashHandler } from "../dispatch.js";

const semantic: SlashHandler = (_args, _loop, ctx) => {
  const root = ctx.codeRoot;
  if (!root) {
    return {
      info: "/semantic is only available inside `reasonix code` (needs a project root).",
    };
  }
  // Fire-and-forget: probes (file stat, optional Ollama HTTP) take
  // ~50–200ms which is too long to block the prompt. Same pattern
  // /kill uses — return a placeholder, post the rich result through
  // ctx.postInfo when ready. ctx.postInfo is wired by the TUI when
  // it owns historical rendering.
  void (async () => {
    const status = await renderSemanticStatus(root);
    ctx.postInfo?.(status);
  })();
  return { info: "▸ checking semantic_search status…" };
};

export async function renderSemanticStatus(rootDir: string): Promise<string> {
  const lines: string[] = [t("slashHeader"), ""];
  const indexExists = await indexFileExists(rootDir);
  if (indexExists) {
    const meta = await readIndexMeta(rootDir);
    lines.push(t("slashEnabled"));
    if (meta) {
      lines.push(
        t("slashEnabledDetail", {
          chunks: meta.chunks,
          files: meta.files,
        }),
      );
    }
    lines.push(t("slashEnabledHowto"));
    return lines.join("\n");
  }
  // Not built yet. Walk the prerequisites in priority order.
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

async function indexFileExists(rootDir: string): Promise<boolean> {
  try {
    await fs.access(path.join(rootDir, ".reasonix", "semantic", "index.meta.json"));
    return true;
  } catch {
    return false;
  }
}

interface IndexSummary {
  chunks: number;
  files: number;
}

/** 10MB read cap so `/semantic` stays snappy on very large repos. */
async function readIndexMeta(rootDir: string): Promise<IndexSummary | null> {
  const dataPath = path.join(rootDir, ".reasonix", "semantic", "index.jsonl");
  let raw: string;
  try {
    // Bind size check and content to the same fd so a concurrent
    // rebuild can't grow the file past the 10MB threshold between
    // stat and read (CodeQL js/file-system-race).
    const fh = await fs.open(dataPath, "r");
    try {
      const stat = await fh.stat();
      if (stat.size > 10 * 1024 * 1024) {
        // For huge indexes, give an order-of-magnitude estimate from
        // file size (avg ~500 bytes/chunk in practice). Files won't
        // be available, so we report just the chunk approximation.
        return { chunks: Math.round(stat.size / 500), files: 0 };
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
        /* tolerated — store rebuilds drop bad lines */
      }
    }
    return { chunks, files: seenPaths.size };
  } catch {
    return null;
  }
}

export const handlers: Record<string, SlashHandler> = {
  semantic,
};
