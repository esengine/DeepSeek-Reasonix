import { spawn } from "node:child_process";
import path from "node:path";

const CONTRACT_VERSION = 1;
const EXPECTED_VERSION = "0.6.0";
const MAX_ASSETS = 100;
const MAX_EVIDENCE_PER_ASSET = 20;
const MAX_INPUT_BYTES = 1024 * 1024;
const MAX_OUTPUT_BYTES = 512 * 1024;

function text(value, limit) {
  return String(value ?? "").trim().slice(0, limit);
}

function finiteScore(value) {
  const score = Number(value);
  return Number.isFinite(score) ? Math.max(0, Math.min(1, score)) : 0;
}

function projectAsset(asset) {
  return {
    id: text(asset?.id, 120),
    title: text(asset?.title, 240),
    type: text(asset?.type, 100),
    summary: text(asset?.summary, 1_200),
    owner: text(asset?.owner, 120),
    sensitivity: text(asset?.sensitivity, 40),
    confidence: finiteScore(asset?.confidence),
    tags: Array.isArray(asset?.tags) ? asset.tags.map((tag) => text(tag, 80)).filter(Boolean).slice(0, 20) : [],
    document: {
      title: text(asset?.document?.title, 240),
      sourceName: text(asset?.document?.sourceName, 240),
      sha256: text(asset?.document?.sha256, 64),
    },
    evidence: Array.isArray(asset?.evidence) ? asset.evidence.slice(0, MAX_EVIDENCE_PER_ASSET).map((item) => ({
      id: text(item?.id, 120),
      section: text(item?.section, 200),
      locator: text(item?.locator, 200),
      sha256: text(item?.sha256, 64),
    })).filter((item) => item.id) : [],
  };
}

function semanticError(code, message) {
  return Object.assign(new Error(message), { code });
}

function minimalChildEnvironment() {
  const inherited = process.env;
  return Object.fromEntries(Object.entries({
    SystemRoot: inherited.SystemRoot,
    WINDIR: inherited.WINDIR,
    TEMP: inherited.TEMP,
    TMP: inherited.TMP,
    LANG: inherited.LANG,
  }).filter(([, value]) => value != null && value !== ""));
}

function validRuntimePaths({ pythonPath, sourcePath, bridgePath }) {
  return [pythonPath, sourcePath, bridgePath].every((value) => typeof value === "string" && path.isAbsolute(value))
    && !sourcePath.split(path.delimiter).slice(1).length;
}

export async function runSemanticaProcess({ pythonPath, sourcePath, bridgePath, timeoutMs, payload }) {
  if (!validRuntimePaths({ pythonPath, sourcePath, bridgePath })) throw semanticError("SEMANTICA_INVALID_CONFIGURATION", "Semantic runtime requires single absolute paths");
  const input = JSON.stringify(payload);
  if (Buffer.byteLength(input, "utf8") > MAX_INPUT_BYTES) throw semanticError("SEMANTICA_INPUT_TOO_LARGE", "Semantic request exceeds the local bridge limit");
  return new Promise((resolve, reject) => {
    let settled = false;
    let stdout = "";
    let stderr = "";
    const child = spawn(pythonPath, ["-I", "-X", "utf8", bridgePath, sourcePath], {
      shell: false,
      windowsHide: true,
      stdio: ["pipe", "pipe", "pipe"],
      env: minimalChildEnvironment(),
    });
    const finish = (callback) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      callback();
    };
    const timer = setTimeout(() => {
      child.kill();
      finish(() => reject(semanticError("SEMANTICA_TIMEOUT", "Local semantic check timed out")));
    }, Math.max(1_000, Number(timeoutMs) || 15_000));
    child.once("error", () => finish(() => reject(semanticError("SEMANTICA_PROCESS_FAILED", "Local semantic process could not start"))));
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
      if (Buffer.byteLength(stdout, "utf8") > MAX_OUTPUT_BYTES) {
        child.kill();
        finish(() => reject(semanticError("SEMANTICA_OUTPUT_TOO_LARGE", "Local semantic output exceeds the gateway limit")));
      }
    });
    child.stderr.on("data", (chunk) => { stderr = `${stderr}${chunk}`.slice(-131_072); });
    child.once("close", (code) => finish(() => {
      if (code !== 0) {
        reject(semanticError("SEMANTICA_PROCESS_FAILED", `Local semantic process exited with code ${code}`));
        return;
      }
      try {
        resolve(JSON.parse(stdout));
      } catch {
        reject(semanticError("SEMANTICA_INVALID_OUTPUT", "Local semantic process returned invalid JSON"));
      }
    }));
    child.stdin.end(input, "utf8");
  });
}

function validateEnvelope(output, expectedStatus) {
  if (!output || typeof output !== "object" || output.contractVersion !== CONTRACT_VERSION || output.engine !== "Semantica") {
    throw semanticError("SEMANTICA_INVALID_OUTPUT", "Semantic bridge contract validation failed");
  }
  if (output.version !== EXPECTED_VERSION) throw semanticError("SEMANTICA_VERSION_MISMATCH", `Semantica version ${output.version || "unknown"} does not match ${EXPECTED_VERSION}`);
  if (output.status !== expectedStatus) throw semanticError("SEMANTICA_INVALID_OUTPUT", "Semantic bridge returned an unexpected state");
  return output;
}

function validateResult(output, authorizedIds) {
  validateEnvelope(output, "complete");
  const assertAuthorized = (id) => {
    if (!authorizedIds.has(String(id))) throw semanticError("SEMANTICA_INVALID_OUTPUT", "Semantic bridge referenced an unauthorized asset");
    return String(id);
  };
  const duplicates = (Array.isArray(output.duplicates) ? output.duplicates : []).slice(0, 50).map((candidate) => {
    if (!Array.isArray(candidate?.assetIds) || candidate.assetIds.length !== 2) throw semanticError("SEMANTICA_INVALID_OUTPUT", "Semantic duplicate candidate is invalid");
    return {
      assetIds: candidate.assetIds.map(assertAuthorized),
      similarity: finiteScore(candidate.similarity),
      confidence: finiteScore(candidate.confidence),
      reasons: Array.isArray(candidate.reasons) ? candidate.reasons.map((reason) => text(reason, 100)).filter(Boolean).slice(0, 8) : [],
    };
  });
  const conflicts = (Array.isArray(output.conflicts) ? output.conflicts : []).slice(0, 50).map((conflict) => ({
    group: text(conflict?.group, 80),
    title: text(conflict?.title, 240),
    field: text(conflict?.field, 60),
    severity: ["low", "medium", "high", "critical"].includes(conflict?.severity) ? conflict.severity : "medium",
    confidence: finiteScore(conflict?.confidence),
    values: Array.isArray(conflict?.values) ? conflict.values.map((value) => text(value, 240)).filter(Boolean).slice(0, 10) : [],
    sources: (Array.isArray(conflict?.sources) ? conflict.sources : []).slice(0, 20).map((source) => ({
      assetId: assertAuthorized(source?.assetId),
      document: text(source?.document, 240),
      value: text(source?.value, 240),
    })),
  }));
  const provenanceEntries = (Array.isArray(output.provenance?.entries) ? output.provenance.entries : []).slice(0, MAX_ASSETS).map((entry) => ({
    assetId: assertAuthorized(entry?.assetId),
    source: text(entry?.source, 240),
    checksum: text(entry?.checksum, 64),
    evidenceCount: Math.max(0, Math.min(MAX_EVIDENCE_PER_ASSET, Number(entry?.evidenceCount) || 0)),
  }));
  return {
    contractVersion: CONTRACT_VERSION,
    status: "complete",
    engine: "Semantica",
    version: EXPECTED_VERSION,
    checkedAssets: Math.max(0, Math.min(authorizedIds.size, Number(output.checkedAssets) || 0)),
    duplicates,
    conflicts,
    provenance: {
      assets: Math.max(0, Math.min(authorizedIds.size, Number(output.provenance?.assets) || 0)),
      evidence: Math.max(0, Number(output.provenance?.evidence) || 0),
      entries: provenanceEntries,
    },
  };
}

export function createSemanticaClient(options = {}) {
  const enabled = options.enabled === true;
  const runner = options.runner ?? runSemanticaProcess;
  const runtime = {
    pythonPath: options.pythonPath,
    sourcePath: options.sourcePath,
    bridgePath: options.bridgePath,
    timeoutMs: options.timeoutMs ?? 15_000,
  };
  let statusCache = null;
  let statusCachedAt = 0;

  async function status({ force = false } = {}) {
    if (!enabled) return { state: "disabled", enabled: false, engine: "Semantica", version: null, message: "未启用可选语义增强" };
    if (!validRuntimePaths(runtime)) return { state: "unavailable", enabled: true, engine: "Semantica", version: null, message: "语义增强运行路径必须是单一绝对路径" };
    if (!force && statusCache && Date.now() - statusCachedAt < 30_000) return statusCache;
    try {
      const output = validateEnvelope(await runner({ ...runtime, payload: { contractVersion: CONTRACT_VERSION, action: "status" } }), "ready");
      statusCache = { state: "ready", enabled: true, engine: "Semantica", version: output.version, message: "本地语义增强可用", capabilities: Array.isArray(output.capabilities) ? output.capabilities.slice(0, 10) : [] };
    } catch (error) {
      const message = error?.code === "SEMANTICA_VERSION_MISMATCH" ? `版本不符合要求：需要 ${EXPECTED_VERSION}` : error?.code === "SEMANTICA_TIMEOUT" ? "语义增强启动超时" : "本地语义增强暂不可用";
      statusCache = { state: "unavailable", enabled: true, engine: "Semantica", version: null, message };
    }
    statusCachedAt = Date.now();
    return statusCache;
  }

  async function enrich(assets) {
    if (!enabled) throw semanticError("SEMANTICA_DISABLED", "Optional semantic enhancement is disabled");
    const readiness = await status();
    if (readiness.state !== "ready") throw semanticError("SEMANTICA_UNAVAILABLE", readiness.message);
    const projected = (Array.isArray(assets) ? assets : []).slice(0, MAX_ASSETS).map(projectAsset).filter((asset) => asset.id && asset.title);
    const authorizedIds = new Set(projected.map((asset) => asset.id));
    try {
      const output = await runner({ ...runtime, payload: { contractVersion: CONTRACT_VERSION, action: "enrich", assets: projected } });
      return validateResult(output, authorizedIds);
    } catch (error) {
      if (["SEMANTICA_TIMEOUT", "SEMANTICA_PROCESS_FAILED", "SEMANTICA_OUTPUT_TOO_LARGE"].includes(error?.code)) throw semanticError(error.code, "本地语义增强暂不可用");
      throw error;
    }
  }

  return { status, enrich, limits: { assets: MAX_ASSETS, evidencePerAsset: MAX_EVIDENCE_PER_ASSET, inputBytes: MAX_INPUT_BYTES, outputBytes: MAX_OUTPUT_BYTES } };
}
