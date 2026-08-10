import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";

function hash(value) {
  return createHash("sha256").update(String(value)).digest("hex");
}

function safeText(value, fallback = "") {
  return typeof value === "string" && value.trim() ? value.trim().slice(0, 2_000) : fallback;
}

function publicId(prefix, seed, size = 12) {
  return `${prefix}-${hash(seed).slice(0, size).toUpperCase()}`;
}

function emptyStore() {
  return { schemaVersion: 1, publications: [] };
}

export function createPublicationRegistry(options = {}) {
  const rootDir = path.resolve(options.rootDir ?? path.resolve(process.cwd(), ".runtime", "publications"));
  const storeFile = path.join(rootDir, "registry.json");
  const platformStore = options.store ?? null;
  const defaultWorkspaceId = options.defaultWorkspaceId ?? "WS-DEMO";
  let writeQueue = Promise.resolve();

  async function readStore(workspaceId = defaultWorkspaceId) {
    if (platformStore) return { schemaVersion: 1, publications: platformStore.listPublications(workspaceId) };
    try {
      const parsed = JSON.parse(await readFile(storeFile, "utf8"));
      return Array.isArray(parsed?.publications) ? parsed : emptyStore();
    } catch (error) {
      if (error?.code === "ENOENT") return emptyStore();
      throw new Error("Publication registry could not be read");
    }
  }

  async function writeStore(store) {
    if (platformStore) throw new Error("Platform publications must be saved transactionally");
    await mkdir(rootDir, { recursive: true });
    const temporary = path.join(rootDir, `.registry-${randomUUID()}.tmp`);
    await writeFile(temporary, `${JSON.stringify(store, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
    await rename(temporary, storeFile);
  }

  function makePublication(job, metadata) {
    if (job?.state !== "complete" || !job?.result?.analysis) throw new Error("Only completed analysis jobs can be published");
    const analysis = job.result.analysis;
    const parser = job.result.parser ?? {};
    const llm = job.result.llm ?? {};
    const publishedAt = new Date().toISOString();
    const publicationId = publicId("PUB", job.id);
    const document = {
      title: safeText(analysis.document?.title, job.document?.name || "未命名文档"),
      category: safeText(analysis.document?.category, job.document?.expectedCategory || "待复核"),
      summary: safeText(analysis.document?.summary, "暂无摘要"),
      language: safeText(analysis.document?.language, "zh-CN"),
      sourceName: safeText(job.document?.name, "未命名文档"),
      markdownSha256: safeText(parser.markdownSha256),
      parserProvider: safeText(parser.provider, "MinerU"),
      parserModel: safeText(parser.model),
      parserBatchId: safeText(parser.batchId),
      llmProvider: safeText(llm.provider, "DeepSeek"),
      llmModel: safeText(llm.model),
    };
    const assets = (Array.isArray(analysis.assets) ? analysis.assets : []).map((asset, assetIndex) => {
      const assetId = publicId("IP-REAL", `${job.id}:${asset.id || assetIndex}`);
      const evidence = (Array.isArray(asset.source_quotes) ? asset.source_quotes : []).map((source, evidenceIndex) => {
        const quote = safeText(source?.quote);
        const section = safeText(source?.section, "MinerU Markdown");
        return {
          id: publicId("EV", `${assetId}:${evidenceIndex}:${quote}`),
          assetId,
          quote,
          section,
          quoteHash: hash(quote),
          documentHash: document.markdownSha256,
          documentName: document.sourceName,
          parserBatchId: document.parserBatchId,
          precision: "章节级",
          locator: section,
          verified: Boolean(quote && document.markdownSha256),
        };
      });
      return {
        id: assetId,
        sourceAssetId: safeText(asset.id, `IP-${assetIndex + 1}`),
        publicationId,
        sourceJobId: job.id,
        title: safeText(asset.title, `未命名 IP 资产 ${assetIndex + 1}`),
        type: safeText(asset.type, "知识资产"),
        summary: safeText(asset.summary, document.summary),
        tags: Array.isArray(asset.tags) ? asset.tags.map((tag) => safeText(tag)).filter(Boolean).slice(0, 8) : [],
        confidence: Math.max(0, Math.min(1, Number(asset.confidence) || 0)),
        owner: safeText(metadata.owner, "待确权"),
        sensitivity: safeText(metadata.sensitivity, "待复核"),
        status: "已发布",
        version: "V1.0",
        publishedAt,
        document,
        evidence,
        wiki: {
          title: safeText(asset.title, document.title),
          executiveSummary: safeText(analysis.wiki?.executive_summary, asset.summary || document.summary),
          keyMechanism: safeText(analysis.wiki?.key_mechanism, "暂无"),
          metrics: Array.isArray(analysis.wiki?.metrics) ? analysis.wiki.metrics.slice(0, 12) : [],
          relationships: Array.isArray(analysis.wiki?.relationships) ? analysis.wiki.relationships.slice(0, 12) : [],
        },
      };
    });
    return { publicationId, sourceJobId: job.id, status: "published", version: "V1.0", publishedAt, document, assets };
  }

  return {
    async publish(job, metadata = {}, workspaceId = defaultWorkspaceId) {
      if (platformStore) {
        const existing = platformStore.listPublications(workspaceId).find((item) => item.sourceJobId === job?.id);
        if (existing) return structuredClone(existing);
        return platformStore.savePublication(workspaceId, makePublication(job, metadata));
      }
      let result;
      writeQueue = writeQueue.then(async () => {
        const store = await readStore();
        const existing = store.publications.find((item) => item.sourceJobId === job?.id);
        if (existing) { result = existing; return; }
        result = makePublication(job, metadata);
        store.publications.unshift(result);
        await writeStore(store);
      });
      await writeQueue;
      return structuredClone(result);
    },
    async listAssets(workspaceId = defaultWorkspaceId) {
      const store = await readStore(workspaceId);
      return store.publications.flatMap((publication) => publication.assets).sort((a, b) => b.publishedAt.localeCompare(a.publishedAt));
    },
    async getAsset(first, second) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      if (platformStore) return platformStore.findAsset(workspaceId, id);
      return (await this.listAssets(workspaceId)).find((asset) => asset.id === id) ?? null;
    },
    async getWiki(first, second) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      const asset = await this.getAsset(workspaceId, id);
      if (!asset) return null;
      const current = platformStore?.getWiki(workspaceId, id);
      return { assetId: asset.id, publicationId: asset.publicationId, version: current?.version ?? asset.version, publishedAt: asset.publishedAt, updatedAt: current?.updatedAt ?? asset.publishedAt, owner: asset.owner, sensitivity: asset.sensitivity, confidence: asset.confidence, document: asset.document, evidence: asset.evidence, ...(current ?? asset.wiki) };
    },
    async getEvidence(first, second) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      for (const asset of await this.listAssets(workspaceId)) {
        const evidence = asset.evidence.find((item) => item.id === id);
        if (evidence) return { ...evidence, assetTitle: asset.title };
      }
      return null;
    },
    async search(query, workspaceId = defaultWorkspaceId) {
      const normalized = safeText(query).toLocaleLowerCase("zh-CN");
      if (!normalized) return [];
      return (await this.listAssets(workspaceId)).filter((asset) => [asset.id, asset.title, asset.type, asset.summary, asset.owner, asset.document?.title, asset.document?.sourceName, asset.wiki?.title, asset.wiki?.executiveSummary, asset.wiki?.keyMechanism, ...asset.tags, ...asset.evidence.map((item) => `${item.section} ${item.quote}`)].join(" ").toLocaleLowerCase("zh-CN").includes(normalized));
    },
    async updateWiki(workspaceId, assetId, input) {
      if (!platformStore) throw new Error("Wiki editing requires the transactional platform store");
      if (!platformStore.findAsset(workspaceId, assetId)) return null;
      return platformStore.saveWikiVersion(workspaceId, assetId, input);
    },
    async listWikiVersions(workspaceId, assetId) {
      if (!platformStore) {
        const wiki = await this.getWiki(workspaceId, assetId);
        return wiki ? [wiki] : [];
      }
      if (!platformStore.findAsset(workspaceId, assetId)) return [];
      return platformStore.listWikiVersions(workspaceId, assetId);
    },
  };
}
