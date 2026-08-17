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

function canViewAsset(asset, role = "owner") {
  if (role !== "viewer") return true;
  return !new Set(["机密", "绝密", "confidential", "secret", "restricted"])
    .has(String(asset?.sensitivity ?? "").normalize("NFKC").trim().toLocaleLowerCase("zh-CN"));
}

export function createPublicationRegistry(options = {}) {
  const rootDir = path.resolve(options.rootDir ?? path.resolve(process.cwd(), ".runtime", "publications"));
  const storeFile = path.join(rootDir, "registry.json");
  const platformStore = options.store ?? null;
  const defaultWorkspaceId = options.defaultWorkspaceId ?? "WS-DEMO";
  let writeQueue = Promise.resolve();

  async function readLegacyStore() {
    try {
      const parsed = JSON.parse(await readFile(storeFile, "utf8"));
      if (!Array.isArray(parsed?.publications)) throw new Error("Legacy publication registry has an invalid schema");
      return parsed;
    } catch (error) {
      if (error?.code === "ENOENT") return emptyStore();
      if (String(error?.message).startsWith("Legacy publication registry")) throw error;
      throw new Error("Legacy publication registry could not be read");
    }
  }

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
          verified: source?.verified !== false && Boolean(quote && document.markdownSha256),
        };
      }).filter((item) => item.verified);
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
    async migrateLegacyPublications(workspaceId = defaultWorkspaceId) {
      if (!platformStore) return { discovered: 0, imported: 0, skipped: 0 };
      const legacy = await readLegacyStore();
      const existingJobs = new Set(platformStore.listPublications(workspaceId).map((item) => String(item.sourceJobId)));
      let imported = 0;
      let skipped = 0;
      for (const publication of legacy.publications) {
        const sourceJobId = String(publication?.sourceJobId ?? "");
        if (!sourceJobId || !publication?.publicationId || !Array.isArray(publication?.assets)) {
          throw new Error("Legacy publication registry contains an invalid publication");
        }
        if (existingJobs.has(sourceJobId)) {
          skipped += 1;
          continue;
        }
        platformStore.savePublication(workspaceId, publication);
        existingJobs.add(sourceJobId);
        imported += 1;
      }
      return { discovered: legacy.publications.length, imported, skipped };
    },
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
    async listAssets(workspaceId = defaultWorkspaceId, optionsForList = {}) {
      const store = await readStore(workspaceId);
      return store.publications
        .flatMap((publication) => publication.assets)
        .filter((asset) => canViewAsset(asset, optionsForList.role))
        .map((asset) => {
          const current = platformStore?.getWiki(workspaceId, asset.id);
          if (!current) return asset;
          return { ...asset, title: current.title || asset.title, version: current.version, updatedAt: current.updatedAt, wiki: { title: current.title, executiveSummary: current.executiveSummary, keyMechanism: current.keyMechanism, metrics: current.metrics, relationships: current.relationships, updatedAt: current.updatedAt } };
        })
        .sort((a, b) => String(b.publishedAt ?? "").localeCompare(String(a.publishedAt ?? "")));
    },
    async getAsset(first, second, optionsForAsset = {}) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      const asset = platformStore
        ? platformStore.findAsset(workspaceId, id)
        : (await this.listAssets(workspaceId)).find((candidate) => candidate.id === id) ?? null;
      return asset && canViewAsset(asset, optionsForAsset.role) ? asset : null;
    },
    async getWiki(first, second, optionsForWiki = {}) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      const asset = await this.getAsset(workspaceId, id, optionsForWiki);
      if (!asset) return null;
      const current = platformStore?.getWiki(workspaceId, id);
      return { assetId: asset.id, publicationId: asset.publicationId, version: current?.version ?? asset.version, publishedAt: asset.publishedAt, updatedAt: current?.updatedAt ?? asset.publishedAt, owner: asset.owner, sensitivity: asset.sensitivity, confidence: asset.confidence, document: asset.document, evidence: asset.evidence, ...(current ?? asset.wiki) };
    },
    async getEvidence(first, second, optionsForEvidence = {}) {
      const [workspaceId, id] = second === undefined ? [defaultWorkspaceId, first] : [first, second];
      for (const asset of await this.listAssets(workspaceId, optionsForEvidence)) {
        const evidence = asset.evidence.find((item) => item.id === id);
        if (evidence) return { ...evidence, assetTitle: asset.title };
      }
      return null;
    },
    async search(query, workspaceId = defaultWorkspaceId, optionsForSearch = {}) {
      const normalized = safeText(query).toLocaleLowerCase("zh-CN");
      if (!normalized) return [];
      return (await this.listAssets(workspaceId, optionsForSearch)).filter((asset) => [asset.id, asset.title, asset.type, asset.summary, asset.owner, asset.document?.title, asset.document?.sourceName, asset.wiki?.title, asset.wiki?.executiveSummary, asset.wiki?.keyMechanism, ...(Array.isArray(asset.tags) ? asset.tags : []), ...(Array.isArray(asset.evidence) ? asset.evidence : []).map((item) => `${item.section} ${item.quote}`)].join(" ").toLocaleLowerCase("zh-CN").includes(normalized));
    },
    async getAssetGraph(workspaceId = defaultWorkspaceId, optionsForGraph = {}) {
      if (platformStore) return platformStore.getAssetGraph(workspaceId, optionsForGraph);
      const assets = await this.listAssets(workspaceId, optionsForGraph);
      const nodes = assets.map((asset) => ({
        id: asset.id,
        title: asset.title,
        type: asset.type,
        owner: asset.owner,
        sensitivity: asset.sensitivity,
        summary: asset.summary,
        tags: asset.tags ?? [],
        confidence: asset.confidence ?? 0,
        status: asset.status,
        version: asset.version,
        evidenceIds: (asset.evidence ?? []).map((evidence) => evidence.id).filter(Boolean),
        updatedAt: asset.publishedAt,
      }));
      return { nodes: nodes.slice(0, Number(optionsForGraph.limit) || 100), edges: [], meta: { depth: 0, totalVisibleNodes: nodes.length, totalVisibleEdges: 0, truncated: nodes.length > 100, storageMode: "snapshot" } };
    },
    async searchAssetGraph(query, workspaceId = defaultWorkspaceId, optionsForSearch = {}) {
      if (platformStore) return platformStore.searchAssetGraph(workspaceId, query, optionsForSearch);
      const assets = await this.search(query, workspaceId, optionsForSearch);
      return { query, results: assets.map((asset) => ({ asset, matchKind: "direct", score: 66, explanation: "资产内容匹配", path: [asset.id] })), meta: { directMatches: assets.length, expandedMatches: 0, depth: 0 } };
    },
    async updateAssetMetadata(workspaceId, assetId, input, optionsForUpdate = {}) {
      if (!platformStore) throw Object.assign(new Error("Asset governance requires the transactional platform store"), { code: "STORAGE_UNAVAILABLE" });
      const updated = platformStore.updateAssetMetadata(workspaceId, assetId, input, optionsForUpdate);
      if (!updated) return null;
      return this.getAsset(workspaceId, assetId, optionsForUpdate);
    },
    async updateAssetMetadataBatch(workspaceId, assetIds, input, optionsForUpdate = {}) {
      if (!platformStore) throw Object.assign(new Error("Asset governance requires the transactional platform store"), { code: "STORAGE_UNAVAILABLE" });
      const updated = platformStore.updateAssetMetadataBatch(workspaceId, assetIds, input, optionsForUpdate);
      return Promise.all(updated.map((asset) => this.getAsset(workspaceId, asset.id, optionsForUpdate)));
    },
    async createAssetRelationship(workspaceId, input, optionsForRelationship = {}) {
      if (!platformStore) throw Object.assign(new Error("Relationship editing requires the transactional platform store"), { code: "STORAGE_UNAVAILABLE" });
      return platformStore.createAssetRelationship(workspaceId, input, optionsForRelationship);
    },
    async getAssetRelationship(workspaceId, relationshipId, optionsForRelationship = {}) {
      if (!platformStore) return null;
      const relationship = platformStore.getAssetRelationship(workspaceId, relationshipId);
      if (!relationship) return null;
      if (optionsForRelationship.role === "viewer" && relationship.verificationStatus !== "confirmed") return null;
      const [source, target] = await Promise.all([
        this.getAsset(workspaceId, relationship.sourceAssetId, optionsForRelationship),
        this.getAsset(workspaceId, relationship.targetAssetId, optionsForRelationship),
      ]);
      return source && target ? relationship : null;
    },
    async updateAssetRelationshipStatus(workspaceId, relationshipId, status, optionsForRelationship = {}) {
      if (!platformStore) throw Object.assign(new Error("Relationship editing requires the transactional platform store"), { code: "STORAGE_UNAVAILABLE" });
      return platformStore.updateAssetRelationshipStatus(workspaceId, relationshipId, status, optionsForRelationship);
    },
    async updateWiki(workspaceId, assetId, input) {
      if (!platformStore) throw new Error("Wiki editing requires the transactional platform store");
      if (!platformStore.findAsset(workspaceId, assetId)) return null;
      return platformStore.saveWikiVersion(workspaceId, assetId, input);
    },
    async submitWikiReview(workspaceId, assetId, input) {
      if (!platformStore) throw new Error("Wiki review requires the transactional platform store");
      if (!platformStore.findAsset(workspaceId, assetId)) return null;
      return platformStore.submitWikiReview(workspaceId, assetId, input);
    },
    async listWikiReviews(workspaceId, options = {}) {
      if (!platformStore) return [];
      return platformStore.listWikiReviews(workspaceId, options);
    },
    async decideWikiReview(workspaceId, reviewId, input) {
      if (!platformStore) throw new Error("Wiki review requires the transactional platform store");
      return platformStore.decideWikiReview(workspaceId, reviewId, input);
    },
    async listWikiVersions(workspaceId, assetId, optionsForWiki = {}) {
      if (!platformStore) {
        const wiki = await this.getWiki(workspaceId, assetId, optionsForWiki);
        return wiki ? [wiki] : [];
      }
      if (!await this.getAsset(workspaceId, assetId, optionsForWiki)) return [];
      return platformStore.listWikiVersions(workspaceId, assetId);
    },
  };
}
