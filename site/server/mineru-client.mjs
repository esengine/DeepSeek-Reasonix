import path from "node:path";
import { extractFullMarkdown } from "./zip-reader.mjs";
import { downloadHttpsViaProxy } from "./proxy-download.mjs";

const ALLOWED_EXTENSIONS = new Set([".pdf", ".doc", ".docx", ".ppt", ".pptx", ".png", ".jpg", ".jpeg", ".webp", ".html"]);
const PROVIDER_HOST_SUFFIXES = [".mineru.net", ".openxlab.org.cn", ".aliyuncs.com"];
export const MAX_UPLOAD_BYTES = 25 * 1024 * 1024;

export function validateDocumentUpload(file) {
  const name = path.basename(String(file?.name ?? "")).replace(/[^\p{L}\p{N}._ -]/gu, "_").slice(0, 180);
  const bytes = Buffer.isBuffer(file?.bytes) ? file.bytes : Buffer.from(file?.bytes ?? []);
  const extension = path.extname(name).toLowerCase();
  if (!name || !extension || !ALLOWED_EXTENSIONS.has(extension)) throw new Error("Unsupported document type for MinerU");
  if (!bytes.length) throw new Error("Document is empty");
  if (bytes.length > MAX_UPLOAD_BYTES) throw new Error("Document exceeds the 25 MB gateway limit");
  if (extension === ".html") {
    const prefix = bytes.subarray(0, 4096).toString("utf8").toLowerCase();
    if (!prefix.includes("<html") && !prefix.includes("<!doctype html")) throw new Error("HTML file signature is invalid");
  }
  if (extension === ".pdf" && bytes.subarray(0, 5).toString("ascii") !== "%PDF-") throw new Error("PDF file signature is invalid");
  return { name, bytes, extension };
}

function validateProviderUrl(value, purpose) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`MinerU ${purpose} URL is invalid`);
  }
  const hostname = parsed.hostname.toLowerCase();
  const allowed = PROVIDER_HOST_SUFFIXES.some((suffix) => hostname === suffix.slice(1) || hostname.endsWith(suffix));
  if (parsed.protocol !== "https:" || !allowed) throw new Error(`MinerU ${purpose} URL is outside the trusted provider domains`);
  return parsed.toString();
}

async function jsonRequest(fetchImpl, url, options, operation) {
  let response;
  try {
    response = await fetchImpl(url, { ...options, signal: options.signal ?? AbortSignal.timeout(45_000) });
  } catch {
    throw new Error(`MinerU ${operation} request could not be completed`);
  }
  let payload;
  try {
    payload = await response.json();
  } catch {
    throw new Error(`MinerU ${operation} returned an invalid response`);
  }
  if (!response.ok || payload?.code !== 0) {
    const code = payload?.code ?? response.status;
    throw new Error(`MinerU ${operation} failed (${String(code).slice(0, 24)})`);
  }
  return payload;
}

export function createMineruClient(options) {
  const {
    apiKey,
    fetchImpl = fetch,
    baseUrl = "https://mineru.net/api/v4",
    pollIntervalMs = 4_000,
    maxWaitMs = 10 * 60_000,
    archiveProxyUrl = null,
    sleep = (duration) => new Promise((resolve) => setTimeout(resolve, duration)),
  } = options;
  if (!apiKey) throw new Error("MinerU credential is required");
  const authorization = `Bearer ${apiKey}`;

  return {
    async parseDocument(file, hooks = {}) {
      const validated = validateDocumentUpload(file);
      const dataId = hooks.dataId ?? `intelifar-${crypto.randomUUID()}`;
      hooks.onProgress?.({ state: "upload-url", progress: 8 });
      const createPayload = await jsonRequest(fetchImpl, `${baseUrl}/file-urls/batch`, {
        method: "POST",
        headers: { authorization, "content-type": "application/json" },
        body: JSON.stringify({
          files: [{ name: validated.name, data_id: dataId }],
          model_version: validated.extension === ".html" ? "MinerU-HTML" : "vlm",
          language: "ch",
          enable_table: true,
          enable_formula: true,
        }),
      }, "upload initialization");
      const batchId = createPayload?.data?.batch_id;
      const uploadUrlValue = createPayload?.data?.file_urls?.[0];
      if (!batchId || !uploadUrlValue) throw new Error("MinerU upload initialization returned incomplete task data");
      const uploadUrl = validateProviderUrl(uploadUrlValue, "upload");

      hooks.onProgress?.({ state: "uploading", progress: 16, batchId });
      let uploadResponse;
      try {
        uploadResponse = await fetchImpl(uploadUrl, { method: "PUT", body: validated.bytes, signal: AbortSignal.timeout(120_000) });
      } catch {
        throw new Error("MinerU document upload could not be completed");
      }
      if (!uploadResponse.ok) throw new Error(`MinerU document upload failed (${uploadResponse.status})`);

      const startedAt = Date.now();
      let result;
      while (Date.now() - startedAt <= maxWaitMs) {
        const statusPayload = await jsonRequest(fetchImpl, `${baseUrl}/extract-results/batch/${encodeURIComponent(batchId)}`, {
          method: "GET",
          headers: { authorization, accept: "application/json" },
        }, "status polling");
        result = statusPayload?.data?.extract_result?.[0];
        const state = result?.state ?? "pending";
        const extracted = Number(result?.extract_progress?.extracted_pages ?? 0);
        const total = Number(result?.extract_progress?.total_pages ?? 0);
        const fraction = total > 0 ? extracted / total : state === "running" ? 0.5 : 0;
        hooks.onProgress?.({ state, progress: Math.round(24 + fraction * 28), batchId });
        if (state === "done") break;
        if (state === "failed") throw new Error("MinerU extraction failed");
        await sleep(pollIntervalMs);
      }
      if (result?.state !== "done") throw new Error("MinerU extraction timed out");
      if (!result.full_zip_url) throw new Error("MinerU completed without a result archive");
      const archiveUrl = validateProviderUrl(result.full_zip_url, "archive");

      hooks.onProgress?.({ state: "downloading", progress: 56, batchId });
      let archiveResponse;
      let archiveError;
      const downloaders = archiveProxyUrl
        ? [
            () => fetchImpl(archiveUrl, { method: "GET", signal: AbortSignal.timeout(45_000) }),
            () => downloadHttpsViaProxy(archiveUrl, archiveProxyUrl),
            () => downloadHttpsViaProxy(archiveUrl, archiveProxyUrl),
          ]
        : Array.from({ length: 3 }, () => () => fetchImpl(archiveUrl, { method: "GET", signal: AbortSignal.timeout(120_000) }));
      for (let attempt = 0; attempt < downloaders.length; attempt += 1) {
        try {
          archiveResponse = await downloaders[attempt]();
          break;
        } catch (error) {
          archiveError = error;
          if (attempt < downloaders.length - 1) await sleep((attempt + 1) * 1_000);
        }
      }
      if (!archiveResponse) {
        const code = String(archiveError?.cause?.code || archiveError?.code || archiveError?.name || "NETWORK_ERROR").replace(/[^A-Z0-9_-]/gi, "").slice(0, 40);
        throw new Error(`MinerU result download could not be completed (${code})`);
      }
      if (!archiveResponse.ok) throw new Error(`MinerU result download failed (${archiveResponse.status})`);
      const archive = Buffer.from(await archiveResponse.arrayBuffer());
      const markdown = extractFullMarkdown(archive);
      if (!markdown.trim()) throw new Error("MinerU returned empty Markdown");
      return {
        provider: "MinerU",
        model: validated.extension === ".html" ? "MinerU-HTML" : "vlm",
        batchId,
        traceId: createPayload.trace_id ?? null,
        fileName: result.file_name || validated.name,
        markdown,
      };
    },
  };
}
