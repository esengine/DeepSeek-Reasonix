import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const ALLOWED_HOSTS = new Set(["arxiv.org", "export.arxiv.org", "patents.google.com"]);
const MAX_SOURCE_BYTES = 25 * 1024 * 1024;

export function validateCorpusSource(source) {
  if (!source || typeof source !== "object") throw new Error("Corpus source is required");
  const url = new URL(String(source.url));
  if (url.protocol !== "https:" || !ALLOWED_HOSTS.has(url.hostname)) throw new Error("Corpus source host is not allowlisted");
  if (!/^[a-z0-9][a-z0-9._-]{1,80}$/iu.test(String(source.fileName || ""))) throw new Error("Corpus filename is invalid");
  if (!/[.]pdf$/iu.test(source.fileName) && !/[.]html$/iu.test(source.fileName)) throw new Error("Corpus format is unsupported");
  return { ...source, url: url.toString() };
}

function validateBytes(source, bytes, contentType = "") {
  if (!bytes.length || bytes.length > MAX_SOURCE_BYTES) throw new Error("Corpus source exceeds the size limit");
  if (source.format === "pdf") {
    if (bytes.subarray(0, 5).toString("ascii") !== "%PDF-") throw new Error("Corpus PDF signature is invalid");
    if (contentType && !/application\/pdf|application\/octet-stream/iu.test(contentType)) throw new Error("Corpus PDF MIME type is invalid");
  } else {
    const prefix = bytes.subarray(0, 1_024).toString("utf8").trimStart().toLowerCase();
    if (!prefix.startsWith("<!doctype html") && !prefix.startsWith("<html")) throw new Error("Corpus HTML signature is invalid");
    if (contentType && !/text\/html|application\/xhtml\+xml/iu.test(contentType)) throw new Error("Corpus HTML MIME type is invalid");
    const text = bytes.toString("utf8");
    for (const concept of source.expectedConcepts || []) {
      if (!text.toLocaleLowerCase("zh-CN").includes(String(concept).toLocaleLowerCase("zh-CN"))) throw new Error(`Corpus source is missing expected concept: ${concept}`);
    }
  }
  return bytes;
}

async function readBoundedBody(response) {
  const declared = Number(response.headers.get("content-length") || 0);
  if (declared > MAX_SOURCE_BYTES) throw new Error("Corpus source exceeds the size limit");
  if (!response.body) return Buffer.from(await response.arrayBuffer());
  const chunks = [];
  let total = 0;
  const reader = response.body.getReader();
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > MAX_SOURCE_BYTES) {
      await reader.cancel();
      throw new Error("Corpus source exceeds the size limit");
    }
    chunks.push(Buffer.from(value));
  }
  return Buffer.concat(chunks);
}

async function fetchAllowlisted(source, fetchImpl) {
  let current = new URL(source.url);
  for (let redirect = 0; redirect <= 5; redirect += 1) {
    if (current.protocol !== "https:" || !ALLOWED_HOSTS.has(current.hostname)) throw new Error("Corpus redirect host is not allowlisted");
    const response = await fetchImpl(current, {
      redirect: "manual",
      headers: { accept: source.format === "pdf" ? "application/pdf" : "text/html", "user-agent": "intelifar-validation/1.0" },
      signal: AbortSignal.timeout(180_000),
    });
    if (response.status >= 300 && response.status < 400) {
      const location = response.headers.get("location");
      if (!location) throw new Error("Corpus redirect is missing a location");
      current = new URL(location, current);
      continue;
    }
    if (!response.ok) throw new Error(`Corpus source request failed (${response.status})`);
    const bytes = validateBytes(source, await readBoundedBody(response), response.headers.get("content-type") || "");
    return { bytes, finalUrl: current.toString(), contentType: response.headers.get("content-type") || "" };
  }
  throw new Error("Corpus source redirected too many times");
}

export async function prepareCorpusSource(rawSource, options) {
  const source = validateCorpusSource(rawSource);
  const cacheDir = path.resolve(options.cacheDir);
  const target = path.resolve(cacheDir, source.fileName);
  if (!target.startsWith(`${cacheDir}${path.sep}`)) throw new Error("Corpus cache path escaped its root");
  await mkdir(cacheDir, { recursive: true });
  let bytes;
  let finalUrl = source.url;
  let contentType = source.format === "pdf" ? "application/pdf" : "text/html";
  let cached = false;
  if (options.refresh !== true) {
    try {
      bytes = validateBytes(source, await readFile(target));
      cached = true;
    } catch {
      bytes = null;
    }
  }
  if (!bytes) {
    const downloaded = await fetchAllowlisted(source, options.fetchImpl || fetch);
    ({ bytes, finalUrl, contentType } = downloaded);
    await writeFile(target, bytes, { mode: 0o600 });
  }
  return {
    source,
    path: target,
    bytes,
    cached,
    finalUrl,
    contentType,
    size: bytes.length,
    sha256: createHash("sha256").update(bytes).digest("hex"),
  };
}
