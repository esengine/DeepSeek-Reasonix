import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { createFileSecurityService } from "./file-security-service.mjs";

function safeError(error) {
  const message = String(error?.message || "Analysis failed")
    .replace(/Bearer\s+[^\s]+/gi, "Bearer [redacted]")
    .replace(/https?:\/\/\S+/gi, "[redacted-url]")
    .replace(/\b(?:sk-|key-)[A-Za-z0-9_-]{12,}\b/g, "[redacted-key]")
    .slice(0, 220);
  return message || "Analysis failed";
}

function publicJob(job) {
  if (!job) return null;
  return {
    id: job.id,
    state: job.state,
    progress: job.progress,
    stageLabel: job.stageLabel,
    createdAt: job.createdAt,
    updatedAt: job.updatedAt,
    document: job.document,
    retryable: Boolean(job.retryable),
    security: job.security ?? null,
    error: job.error ?? null,
    result: job.result ?? null,
  };
}

export function createAnalysisService(options) {
  const { mineruClient, deepseekClient, retentionMs = 60 * 60_000, jobStore = null } = options;
  if (!mineruClient || !deepseekClient) throw new Error("Analysis providers are required");
  const uploadRoot = path.resolve(options.uploadRoot ?? path.resolve(process.cwd(), ".runtime", "uploads"));
  const defaultWorkspaceId = options.defaultWorkspaceId ?? "WS-DEMO";
  const fileSecurityService = options.fileSecurityService ?? createFileSecurityService();
  const jobs = new Map();
  const promises = new Map();
  if (jobStore) jobStore.markInterruptedJobs();

  function jobKey(workspaceId, id) {
    return `${workspaceId}:${id}`;
  }

  function uploadDirectory(workspaceId) {
    const workspaceHash = createHash("sha256").update(String(workspaceId)).digest("hex").slice(0, 32);
    return path.join(uploadRoot, `workspace-${workspaceHash}`);
  }

  function prune() {
    if (jobStore) return;
    const cutoff = Date.now() - retentionMs;
    for (const [id, job] of jobs) if (Date.parse(job.updatedAt) < cutoff) jobs.delete(id);
  }

  function recordFor(id, workspaceId = defaultWorkspaceId) {
    return jobStore ? jobStore.getJob(workspaceId, id) : (jobs.has(jobKey(workspaceId, id)) ? { job: jobs.get(jobKey(workspaceId, id)), uploadPath: null } : null);
  }

  function persist(workspaceId, job, uploadPath = null) {
    if (jobStore) jobStore.saveJob(workspaceId, job, { uploadPath });
    else jobs.set(jobKey(workspaceId, job.id), job);
  }

  function update(workspaceId, job, patch, uploadPath = null) {
    Object.assign(job, patch, { updatedAt: new Date().toISOString() });
    persist(workspaceId, job, uploadPath);
  }

  async function run(job, file, workspaceId, uploadPath = null) {
    try {
      update(workspaceId, job, { state: "security-scan", progress: 4, stageLabel: "正在检查文件安全", error: null, retryable: false }, uploadPath);
      const security = await fileSecurityService.scan(file);
      const publicSecurity = {
        decision: security.decision,
        level: security.level,
        engine: security.engine,
        findings: security.findings,
        sha256: security.sha256,
        scannedAt: security.scannedAt,
      };
      update(workspaceId, job, { security: publicSecurity }, uploadPath);
      if (jobStore?.appendAudit) {
        jobStore.appendAudit(workspaceId, {
          actorUserId: job.submittedBy || null,
          action: "file.security_scan",
          objectType: "analysis_job",
          objectId: job.id,
          detail: { decision: security.decision, level: security.level, findings: security.findings, documentSha256: security.sha256 },
        });
      }
      if (security.decision !== "allow") {
        update(workspaceId, job, { state: "blocked", progress: 4, stageLabel: "文件安全检查未通过", retryable: false, error: "文件被安全策略拦截，未发送至外部处理器" }, uploadPath);
        if (uploadPath) await rm(uploadPath, { force: true });
        return;
      }
      update(workspaceId, job, { state: "mineru-upload", progress: 8, stageLabel: "MinerU 正在接收文档", error: null, retryable: false }, uploadPath);
      const parsed = await mineruClient.parseDocument(file, {
        dataId: job.id,
        onProgress(event) {
          const label = event.state === "running" ? "MinerU 正在解析版式与语义" : event.state === "downloading" ? "正在下载 MinerU 解析结果" : "MinerU 正在处理文档";
          update(workspaceId, job, {
            state: event.state === "running" ? "mineru-running" : "mineru-upload",
            progress: Math.max(job.progress, Math.min(60, Number(event.progress) || job.progress)),
            stageLabel: label,
            providerTaskId: event.batchId ?? job.providerTaskId,
          }, uploadPath);
        },
      });
      update(workspaceId, job, { state: "deepseek", progress: 68, stageLabel: "DeepSeek 正在提取 IP 与生成 Wiki", providerTaskId: parsed.batchId }, uploadPath);
      const llm = await deepseekClient.analyzeMarkdown({ markdown: parsed.markdown, documentName: parsed.fileName });
      const markdownHash = createHash("sha256").update(parsed.markdown).digest("hex");
      update(workspaceId, job, {
        state: "complete",
        progress: 100,
        stageLabel: "真实分析完成",
        retryable: false,
        result: {
          parser: {
            provider: parsed.provider,
            model: parsed.model,
            batchId: parsed.batchId,
            traceId: parsed.traceId,
            markdownCharacters: parsed.markdown.length,
            analysisInputCharacters: Number(llm.input?.analysisCharacters ?? parsed.markdown.length),
            analysisSelectedSourceCharacters: Number(llm.input?.selectedSourceCharacters ?? parsed.markdown.length),
            analysisSamplingStrategy: llm.input?.strategy ?? "full",
            analysisTotalSections: Number(llm.input?.totalSections ?? 1),
            analysisSelectedSections: Number(llm.input?.selectedSections ?? 1),
            analysisCoveragePositions: Array.isArray(llm.input?.coveragePositions) ? llm.input.coveragePositions : [0],
            quoteValidation: llm.input?.quoteValidation ?? { total: 0, verified: 0, rejected: 0 },
            markdownSha256: markdownHash,
            markdownPreview: parsed.markdown.slice(0, 4_000),
          },
          llm: {
            provider: llm.provider,
            model: llm.model,
            responseId: llm.responseId,
            usage: llm.usage,
          },
          analysis: llm.analysis,
        },
      }, uploadPath);
      if (uploadPath) await rm(uploadPath, { force: true });
    } catch (error) {
      const scannerUnavailable = error?.code === "SCANNER_UNAVAILABLE";
      update(workspaceId, job, {
        state: "failed",
        stageLabel: scannerUnavailable ? "文件安全扫描器暂不可用" : "真实分析失败",
        retryable: Boolean(uploadPath),
        error: safeError(error),
      }, uploadPath);
    } finally {
      file.bytes = Buffer.alloc(0);
    }
  }

  function startRun(job, file, workspaceId, uploadPath) {
    const key = jobKey(workspaceId, job.id);
    const promise = run(job, file, workspaceId, uploadPath).finally(() => promises.delete(key));
    promises.set(key, promise);
  }

  return {
    async submit(file, metadata = {}) {
      prune();
      const workspaceId = String(metadata.workspaceId ?? defaultWorkspaceId);
      const id = `JOB-REAL-${randomUUID()}`;
      const now = new Date().toISOString();
      const job = {
        id,
        state: "queued",
        progress: 2,
        stageLabel: "任务已安全入队",
        createdAt: now,
        updatedAt: now,
        retryable: false,
        submittedBy: metadata.actorUserId ? String(metadata.actorUserId) : null,
        document: {
          name: file.name,
          size: file.bytes.length,
          sha256: createHash("sha256").update(file.bytes).digest("hex"),
          expectedCategory: String(metadata.expectedCategory ?? "自动判断").slice(0, 80),
        },
      };
      let uploadPath = null;
      if (jobStore) {
        const directory = uploadDirectory(workspaceId);
        await mkdir(directory, { recursive: true });
        uploadPath = path.join(directory, `${id}.upload`);
        await writeFile(uploadPath, file.bytes, { mode: 0o600 });
      }
      try {
        persist(workspaceId, job, uploadPath);
      } catch (error) {
        if (uploadPath) await rm(uploadPath, { force: true });
        throw error;
      }
      startRun(job, file, workspaceId, uploadPath);
      return publicJob(job);
    },
    get(id, workspaceId = defaultWorkspaceId) {
      prune();
      return publicJob(recordFor(id, workspaceId)?.job);
    },
    list(workspaceId = defaultWorkspaceId, limit = 50) {
      if (jobStore) return jobStore.listJobs(workspaceId, limit).map((record) => publicJob(record.job));
      prune();
      return [...jobs.entries()].filter(([key]) => key.startsWith(`${workspaceId}:`)).map(([, job]) => publicJob(job)).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).slice(0, limit);
    },
    async retry(id, workspaceId = defaultWorkspaceId) {
      const record = recordFor(id, workspaceId);
      if (!record) return null;
      if (!record.job.retryable || !record.uploadPath || !["failed", "interrupted"].includes(record.job.state)) {
        const error = new Error("Analysis job is not retryable");
        error.code = "NOT_RETRYABLE";
        throw error;
      }
      let bytes;
      try {
        bytes = await readFile(record.uploadPath);
      } catch {
        update(workspaceId, record.job, { retryable: false, error: "保留的上传文件不可用，无法重试" }, record.uploadPath);
        const error = new Error("Retained upload is unavailable");
        error.code = "UPLOAD_UNAVAILABLE";
        throw error;
      }
      update(workspaceId, record.job, { state: "queued", progress: 2, stageLabel: "重试任务已安全入队", retryable: false, error: null, result: null }, record.uploadPath);
      startRun(record.job, { name: record.job.document.name, bytes }, workspaceId, record.uploadPath);
      return publicJob(record.job);
    },
    async whenSettled(id, workspaceId = defaultWorkspaceId) {
      await promises.get(jobKey(workspaceId, id));
      return publicJob(recordFor(id, workspaceId)?.job);
    },
  };
}
