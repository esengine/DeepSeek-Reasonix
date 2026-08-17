import { readFile, stat } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createAgentModelClient } from "./agent-model-client.mjs";
import { createAgentService } from "./agent-service.mjs";
import { createAgentTools } from "./agent-tools.mjs";
import { createAnalysisService } from "./analysis-service.mjs";
import { createAuthService } from "./auth-service.mjs";
import { createBackupService } from "./backup-service.mjs";
import { loadRuntimeConfig } from "./config.mjs";
import { createDeepSeekClient } from "./deepseek-client.mjs";
import { createFileSecurityService } from "./file-security-service.mjs";
import { applySecurityHeaders } from "./http-security.mjs";
import { createMineruClient, MAX_UPLOAD_BYTES, validateDocumentUpload } from "./mineru-client.mjs";
import { createPlatformStore } from "./platform-store.mjs";
import { createPublicationRegistry } from "./publication-registry.mjs";
import { createSemanticaClient } from "./semantica-client.mjs";
import { createShareService } from "./share-service.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_DIST = path.resolve(here, "../dist");
const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".svg": "image/svg+xml",
  ".json": "application/json; charset=utf-8",
  ".xml": "application/xml; charset=utf-8",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
};
const MAX_REQUEST_BYTES = MAX_UPLOAD_BYTES + 512 * 1024;
const MAX_JSON_BYTES = 64 * 1024;
const DEMO_SESSION = { user: { id: "USR-DEMO", email: "demo@intelifar.local", name: "林越", role: "owner" }, workspace: { id: "WS-DEMO", name: "澜图科技" } };

function sendJson(response, statusCode, value) {
  applySecurityHeaders(response, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store" });
  response.writeHead(statusCode);
  response.end(statusCode === 204 ? undefined : JSON.stringify(value));
}

async function readBoundedBody(request, limit = MAX_REQUEST_BYTES) {
  const declared = Number(request.headers["content-length"] ?? 0);
  if (declared > limit) throw new Error("Request exceeds the gateway limit");
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > limit) throw new Error("Request exceeds the gateway limit");
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

async function readJson(request) {
  if (!String(request.headers["content-type"] ?? "").toLowerCase().startsWith("application/json")) throw new Error("Expected an application/json request");
  const body = await readBoundedBody(request, MAX_JSON_BYTES);
  try {
    return JSON.parse(body.toString("utf8") || "{}");
  } catch {
    throw new Error("Invalid JSON request");
  }
}

async function parseMultipart(request) {
  if (!String(request.headers["content-type"] ?? "").startsWith("multipart/form-data")) throw new Error("Expected a multipart document upload");
  const body = await readBoundedBody(request);
  const webRequest = new Request("http://127.0.0.1/api/analysis", { method: "POST", headers: request.headers, body });
  const form = await webRequest.formData();
  const file = form.get("file");
  if (!file || typeof file.arrayBuffer !== "function" || !file.name) throw new Error("Document file is required");
  return {
    file: validateDocumentUpload({ name: file.name, bytes: Buffer.from(await file.arrayBuffer()) }),
    expectedCategory: String(form.get("category") ?? "自动判断").slice(0, 80),
  };
}

async function serveStatic(response, pathname, distRoot) {
  const decoded = decodeURIComponent(pathname);
  let target = path.resolve(distRoot, `.${decoded === "/" ? "/index.html" : decoded}`);
  const relative = path.relative(distRoot, target);
  if (relative.startsWith("..") || path.isAbsolute(relative)) throw new Error("Invalid path");
  if ((await stat(target)).isDirectory()) target = path.join(target, "index.html");
  const body = await readFile(target);
  applySecurityHeaders(response, { "content-type": MIME[path.extname(target).toLowerCase()] || "application/octet-stream", "cache-control": "no-store" });
  response.writeHead(200);
  response.end(body);
}

function safeWikiInput(input) {
  const value = {
    baseVersion: String(input?.baseVersion ?? "").trim(),
    title: String(input?.title ?? "").trim(),
    executiveSummary: String(input?.executiveSummary ?? "").trim(),
    keyMechanism: String(input?.keyMechanism ?? "").trim(),
    changeNote: String(input?.changeNote ?? "内容更新").trim(),
  };
  if (!/^V1\.\d+$/.test(value.baseVersion)) throw new Error("A valid base Wiki version is required");
  if (!value.title || value.title.length > 200) throw new Error("Wiki title must contain 1 to 200 characters");
  if (!value.executiveSummary || value.executiveSummary.length > 4_000) throw new Error("Wiki summary must contain 1 to 4000 characters");
  if (!value.keyMechanism || value.keyMechanism.length > 8_000) throw new Error("Wiki mechanism must contain 1 to 8000 characters");
  if (value.changeNote.length > 200) throw new Error("Wiki change note is too long");
  return value;
}

function safeWikiReviewDecision(input) {
  const decision = String(input?.decision ?? "").trim();
  const reviewNote = String(input?.reviewNote ?? "").trim();
  if (!["approved", "rejected"].includes(decision)) throw new Error("Wiki review decision must be approved or rejected");
  if (reviewNote.length > 500) throw new Error("Wiki review note is too long");
  return { decision, reviewNote };
}

function safeSemanticReviewDecision(input) {
  const decision = String(input?.decision ?? "").normalize("NFKC").trim();
  const reviewNote = String(input?.reviewNote ?? "").normalize("NFKC").replace(/\s+/g, " ").trim();
  if (!["confirmed", "dismissed"].includes(decision)) throw new Error("Semantic review decision must be confirmed or dismissed");
  if (reviewNote.length > 300) throw new Error("Semantic review note is too long");
  return { decision, reviewNote };
}

function semanticReviewKey(kind, item) {
  const value = item?.payload || item || {};
  const assetIds = kind === "duplicate"
    ? (Array.isArray(value.assetIds) ? value.assetIds : [])
    : (Array.isArray(value.sources) ? value.sources.map((source) => source?.assetId) : value.assetIds || []);
  const values = kind === "conflict" ? (Array.isArray(value.values) ? value.values : []) : [];
  const field = kind === "conflict" ? String(value.field || "") : "";
  return [kind, field, ...assetIds.map(String).sort(), ...values.map(String).sort((left, right) => left.localeCompare(right, "zh-CN"))].join("\n");
}

function attachSemanticReviews(result, reviews) {
  const byKey = new Map(reviews.map((review) => [semanticReviewKey(review.kind, { payload: review.payload }), review]));
  return {
    ...result,
    duplicates: (result.duplicates || []).map((candidate) => {
      const review = byKey.get(semanticReviewKey("duplicate", candidate));
      return review ? { ...candidate, reviewId: review.id, reviewStatus: review.status } : candidate;
    }),
    conflicts: (result.conflicts || []).map((conflict) => {
      const review = byKey.get(semanticReviewKey("conflict", conflict));
      return review ? { ...conflict, reviewId: review.id, reviewStatus: review.status } : conflict;
    }),
  };
}

function safeRelationshipInput(input) {
  const value = {
    sourceAssetId: String(input?.sourceAssetId ?? "").trim(),
    targetAssetId: String(input?.targetAssetId ?? "").trim(),
    relationType: String(input?.relationType ?? "").trim(),
    confidence: input?.confidence == null ? undefined : Number(input.confidence),
    evidenceIds: Array.isArray(input?.evidenceIds) ? input.evidenceIds.map((id) => String(id).trim()).filter(Boolean).slice(0, 20) : [],
  };
  if (!/^IP-[A-Za-z0-9-]{3,96}$/.test(value.sourceAssetId) || !/^IP-[A-Za-z0-9-]{3,96}$/.test(value.targetAssetId)) throw new Error("Valid source and target asset IDs are required");
  if (!/^[a-z_]{3,32}$/.test(value.relationType)) throw new Error("A valid relationship type is required");
  if (value.confidence != null && (!Number.isFinite(value.confidence) || value.confidence < 0 || value.confidence > 1)) throw new Error("Relationship confidence must be between 0 and 1");
  if (value.evidenceIds.some((id) => !/^EV-[A-Za-z0-9-]{2,96}$/.test(id))) throw new Error("Evidence IDs are invalid");
  return value;
}

function queryList(url, name) {
  return url.searchParams.getAll(name)
    .flatMap((value) => value.split(","))
    .map((value) => value.trim())
    .filter(Boolean)
    .slice(0, 20);
}

function safeUiAuditInput(input, workspaceId) {
  const eventType = String(input?.eventType ?? "").trim();
  const objectId = String(input?.objectId ?? "").trim();
  if (eventType === "redaction_evidence_view" && objectId === "REDACT-S1-P114-08") {
    return {
      action: "evidence.view",
      objectType: "redaction_evidence",
      objectId,
      detail: { documentId: "DOC-0318", page: 114, locator: "P-114-08", sensitivity: "S1", sourceMode: "demo" },
    };
  }
  if (eventType === "audit_export" && (!objectId || objectId === workspaceId)) {
    return { action: "audit.export", objectType: "audit_ledger", objectId: workspaceId, detail: { format: "csv" } };
  }
  throw new Error("Unsupported UI audit event");
}

export async function createRealAnalysisServer(options = {}) {
  const config = options.config ?? await loadRuntimeConfig({ cwd: options.cwd ?? process.cwd(), keyFile: options.keyFile });
  const mineruClient = options.mineruClient ?? createMineruClient({ apiKey: config.mineruApiKey, archiveProxyUrl: config.httpsProxy, maxWaitMs: options.mineruMaxWaitMs });
  const deepseekClient = options.deepseekClient ?? createDeepSeekClient({ apiKey: config.deepseekApiKey, model: config.deepseekModel });
  const authRequired = options.auth?.required === true;
  const shouldUsePlatformStore = Boolean(options.platformStore || options.databasePath || authRequired);
  const ownsPlatformStore = shouldUsePlatformStore && !options.platformStore;
  const platformStore = options.platformStore ?? (shouldUsePlatformStore ? createPlatformStore({ dbPath: options.databasePath }) : null);
  const authService = platformStore ? createAuthService({ store: platformStore, secureCookies: options.auth?.secureCookies === true, sessionTtlMs: options.auth?.sessionTtlMs }) : null;
  if (authRequired) {
    if (!options.auth?.password) throw new Error("SMB authentication requires a bootstrap password");
    await authService.bootstrap({
      workspaceId: options.auth.workspaceId ?? "WS-PRIMARY",
      workspaceName: options.auth.workspaceName ?? "intelifar 工作空间",
      email: options.auth.email ?? "owner@intelifar.local",
      password: options.auth.password,
      name: options.auth.name ?? "空间所有者",
    });
  }
  const defaultWorkspaceId = authRequired ? options.auth.workspaceId ?? "WS-PRIMARY" : "WS-DEMO";
  if (platformStore && !authRequired) {
    platformStore.ensureWorkspace({ id: defaultWorkspaceId, name: DEMO_SESSION.workspace.name });
    const existingLoopbackUser = platformStore.getUserById(DEMO_SESSION.user.id);
    if (!existingLoopbackUser) {
      platformStore.createUser({
        id: DEMO_SESSION.user.id,
        workspaceId: defaultWorkspaceId,
        email: DEMO_SESSION.user.email,
        name: DEMO_SESSION.user.name,
        role: DEMO_SESSION.user.role,
        passwordHash: "!loopback-authentication-disabled!",
      });
    } else if (existingLoopbackUser.workspaceId !== defaultWorkspaceId || existingLoopbackUser.role !== "owner") {
      throw new Error("Loopback owner identity conflicts with the configured workspace");
    }
  }
  const fileSecurityService = options.fileSecurityService ?? createFileSecurityService({
    externalScanner: options.externalScanner,
    clamAvPath: options.clamAvPath,
    requireExternal: options.requireExternalScanner,
  });
  const analysisService = options.analysisService ?? createAnalysisService({ mineruClient, deepseekClient, fileSecurityService, jobStore: platformStore, uploadRoot: options.uploadRoot, defaultWorkspaceId });
  const publicationRegistry = options.publicationRegistry ?? createPublicationRegistry({ rootDir: options.registryRoot, store: platformStore, defaultWorkspaceId });
  if (platformStore && (options.migrateLegacyPublications === true || options.registryRoot) && typeof publicationRegistry.migrateLegacyPublications === "function") {
    await publicationRegistry.migrateLegacyPublications(defaultWorkspaceId);
  }
  if (platformStore) platformStore.rebuildAssetGraph(defaultWorkspaceId);
  const agentModelClient = options.agentModelClient ?? (platformStore ? createAgentModelClient({ apiKey: config.deepseekApiKey, model: config.deepseekModel, timeoutMs: options.agentModelTimeoutMs }) : null);
  const agentTools = options.agentTools ?? (platformStore ? createAgentTools({ publicationRegistry }) : null);
  const agentService = options.agentService ?? (platformStore ? createAgentService({
    store: platformStore,
    modelClient: agentModelClient,
    tools: agentTools,
    resolveContext: async ({ workspaceId, userId, role }) => {
      if (!authRequired) return { workspaceId, userId, role: role || "owner", active: true };
      const user = platformStore.getUserById(userId);
      return user ? { workspaceId: user.workspaceId, userId: user.id, role: user.role, active: !user.disabledAt } : null;
    },
    onAudit: (event) => {
      platformStore.appendAudit(event.workspaceId, { actorUserId: event.actorUserId, action: event.action, objectType: event.objectType, objectId: event.objectId, detail: event.detail });
    },
  }) : null);
  const shareService = options.shareService ?? (platformStore ? createShareService({ store: platformStore }) : null);
  const backupService = options.backupService ?? (platformStore ? createBackupService({ store: platformStore, backupRoot: options.backupRoot, retention: options.backupRetention }) : null);
  const semanticaClient = options.semanticaClient ?? createSemanticaClient({
    enabled: options.semantica?.enabled === true,
    pythonPath: options.semantica?.pythonPath,
    sourcePath: options.semantica?.sourcePath,
    bridgePath: options.semantica?.bridgePath ?? path.resolve(here, "../../integrations/semantica/bridge.py"),
    timeoutMs: options.semantica?.timeoutMs,
  });
  const distRoot = path.resolve(options.distRoot ?? DEFAULT_DIST);
  const host = options.host ?? "127.0.0.1";
  const analysisRateLimit = Math.max(1, Number(options.analysisRateLimit ?? 8));
  const loginRateLimit = Math.max(1, Number(options.loginRateLimit ?? 6));
  const rateWindows = new Map();
  const loginRateWindows = new Map();
  const publicRateWindows = new Map();
  const agentRateWindows = new Map();
  const semanticRateWindows = new Map();
  const publicAccessRateLimit = Math.max(1, Number(options.publicAccessRateLimit ?? 20));
  const agentRateLimit = Math.max(1, Number(options.agentRateLimit ?? 12));
  const semanticRateLimit = Math.max(1, Number(options.semanticRateLimit ?? 3));
  const loopbackSession = { ...DEMO_SESSION, mode: platformStore ? "loopback-persistent" : "loopback-demo" };

  function allowWindow(windows, identity, limit) {
    const now = Date.now();
    const current = windows.get(identity);
    if (!current || now - current.startedAt >= 60_000) {
      windows.set(identity, { startedAt: now, count: 1 });
      return true;
    }
    current.count += 1;
    return current.count <= limit;
  }

  function sessionFor(request) {
    return authRequired ? authService.getSessionFromRequest(request) : loopbackSession;
  }

  function appendAudit(session, action, objectType, objectId, detail = {}) {
    if (!platformStore) return;
    platformStore.appendAudit(session.workspace.id, { actorUserId: session.user.id, action, objectType, objectId, detail });
  }

  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, `http://${host}`);
      response.setHeader("x-request-id", randomUUID());
      response.setHeader("x-data-boundary", "local-gateway; external-processors=MinerU,DeepSeek");
      const origin = request.headers.origin;
      if (["POST", "PUT", "PATCH", "DELETE"].includes(request.method) && origin && new URL(origin).host !== request.headers.host) {
        sendJson(response, 403, { error: "Cross-origin state changes are not allowed" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/health") {
        const semanticStatus = await semanticaClient.status();
        sendJson(response, 200, {
          status: "ok",
          mode: "real",
          providers: { mineru: "configured", deepseek: "configured", semantica: semanticStatus.state },
          model: config.deepseekModel,
          auth: { required: authRequired, mode: authRequired ? "local-session" : platformStore ? "loopback-persistent" : "loopback-demo" },
          storage: { adapter: platformStore ? "sqlite" : "atomic-json", durableJobs: Boolean(platformStore), wikiVersions: Boolean(platformStore), semanticReviews: Boolean(platformStore), verifiedBackups: Boolean(backupService), memberLifecycle: Boolean(platformStore), secureShares: Boolean(shareService), agentTasks: Boolean(agentService) },
          agent: { available: Boolean(agentService), boundary: "document-ip-wiki-readonly", maxSteps: 6, maxToolCalls: 12, formalKnowledgeMutation: false },
          fileSecurity: fileSecurityService.status(),
          semanticEnhancement: semanticStatus,
          dataBoundary: { gateway: "local", externalProcessors: ["MinerU", "DeepSeek"], localProcessors: semanticStatus.enabled ? ["Semantica"] : [], disclosure: "Documents are sent to MinerU for parsing; bounded parsed text is sent to DeepSeek for structured analysis. Optional semantic governance runs locally on permission-filtered published asset projections." },
        });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/auth/login") {
        if (!authRequired) { sendJson(response, 409, { error: "Authentication is disabled in loopback demo mode" }); return; }
        if (!allowWindow(loginRateWindows, request.socket.remoteAddress || "unknown", loginRateLimit)) {
          response.setHeader("retry-after", "60");
          sendJson(response, 429, { error: "Too many login attempts; retry in 60 seconds" });
          return;
        }
        const input = await readJson(request);
        const result = await authService.login({ email: input.email, password: input.password });
        response.setHeader("set-cookie", result.setCookie);
        const session = authService.getSessionFromRequest({ headers: { cookie: result.setCookie.split(";")[0] } });
        appendAudit(session, "auth.login", "session", session.id, { result: "success" });
        sendJson(response, 200, { session });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/session") {
        const session = sessionFor(request);
        sendJson(response, session ? 200 : 401, session ? { session } : { error: "Authentication required" });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/auth/logout") {
        if (!authRequired) { sendJson(response, 204, null); return; }
        const session = sessionFor(request);
        if (session) appendAudit(session, "auth.logout", "session", session.id, { result: "success" });
        const result = authService.logout(request);
        response.setHeader("set-cookie", result.setCookie);
        sendJson(response, 204, null);
        return;
      }

      if (request.method === "POST" && ["/api/public/invitations/inspect", "/api/public/invitations/accept", "/api/public/shares/inspect", "/api/public/shares/access"].includes(url.pathname)) {
        const publicIdentity = `${request.socket.remoteAddress || "unknown"}:${url.pathname}`;
        if (!allowWindow(publicRateWindows, publicIdentity, publicAccessRateLimit)) {
          response.setHeader("retry-after", "60");
          sendJson(response, 429, { error: "Too many public access attempts; retry in 60 seconds" });
          return;
        }
        const input = await readJson(request);
        if (url.pathname === "/api/public/invitations/inspect") {
          if (!authService) { sendJson(response, 404, { error: "Invitation is invalid or expired" }); return; }
          const invitation = authService.inspectInvitation(input.token);
          sendJson(response, invitation ? 200 : 404, invitation ? { invitation: { email: invitation.email, name: invitation.name, role: invitation.role, expiresAt: invitation.expiresAt } } : { error: "Invitation is invalid or expired" });
          return;
        }
        if (url.pathname === "/api/public/invitations/accept") {
          if (!authService) { sendJson(response, 404, { error: "Invitation is invalid or expired" }); return; }
          const accepted = await authService.acceptInvitation({ token: input.token, password: input.password });
          platformStore.appendAudit(accepted.user.workspaceId, { actorUserId: accepted.user.id, action: "member.invitation_accept", objectType: "user", objectId: accepted.user.id, detail: { invitationId: accepted.invitation.id, role: accepted.user.role } });
          sendJson(response, 201, { member: accepted.user });
          return;
        }
        if (url.pathname === "/api/public/shares/inspect") {
          const share = shareService?.inspect(input.token);
          sendJson(response, share ? 200 : 404, share ? { share } : { error: "Secure share is unavailable" });
          return;
        }
        if (!shareService) { sendJson(response, 404, { error: "Secure share is unavailable" }); return; }
        sendJson(response, 200, shareService.access({ token: input.token, accessCode: input.accessCode }));
        return;
      }

      const session = url.pathname.startsWith("/api/") ? sessionFor(request) : null;
      if (url.pathname.startsWith("/api/") && !session) { sendJson(response, 401, { error: "Authentication required" }); return; }
      const workspaceId = session?.workspace.id ?? defaultWorkspaceId;
      const requireRole = (role) => {
        if (!authRequired || authService.can(session.user.role, role)) return true;
        sendJson(response, 403, { error: "Insufficient workspace role" });
        return false;
      };

      if (request.method === "GET" && url.pathname === "/api/dashboard") {
        if (!requireRole("viewer")) return;
        const [assets, graph] = await Promise.all([
          publicationRegistry.listAssets(workspaceId, { role: session.user.role }),
          publicationRegistry.getAssetGraph(workspaceId, { role: session.user.role, includeProposed: session.user.role !== "viewer", limit: 200, edgeLimit: 400 }),
        ]);
        const jobs = analysisService.list(workspaceId, 200);
        const auditIntegrity = platformStore ? platformStore.verifyAuditChain(workspaceId) : { valid: false, count: 0, unavailable: true };
        const today = new Date().toISOString().slice(0, 10);
        const thisMonth = today.slice(0, 7);
        const riskItems = jobs.flatMap((job) => Array.isArray(job.result?.analysis?.risks) ? job.result.analysis.risks : []);
        const publishedDocumentIds = new Set(assets.map((asset) => asset.sourceJobId || asset.publicationId).filter(Boolean));
        const documentIds = new Set([...publishedDocumentIds, ...jobs.map((job) => job.id)]);
        const completedDocumentIds = new Set([...publishedDocumentIds, ...jobs.filter((job) => job.state === "complete").map((job) => job.id)]);
        const verifiedEvidence = assets.flatMap((asset) => asset.evidence ?? []).filter((evidence) => evidence.verified).length;
        const totalEvidence = assets.flatMap((asset) => asset.evidence ?? []).length;
        const auditEvents = platformStore?.listAuditEvents(workspaceId, 500) ?? [];
        sendJson(response, 200, { dashboard: {
          assets: { total: assets.length, addedThisMonth: assets.filter((asset) => String(asset.publishedAt ?? "").startsWith(thisMonth)).length },
          evidence: { verified: verifiedEvidence, total: totalEvidence, coverage: totalEvidence ? Math.round((verifiedEvidence / totalEvidence) * 1_000) / 10 : 0 },
          documents: { total: documentIds.size, processing: jobs.filter((job) => !["complete", "failed", "interrupted", "cancelled", "blocked"].includes(job.state)).length, complete: completedDocumentIds.size, failed: jobs.filter((job) => ["failed", "interrupted", "blocked"].includes(job.state)).length },
          risks: { total: riskItems.length, high: riskItems.filter((risk) => ["high", "高", "S1"].includes(String(risk?.level ?? risk?.severity))).length },
          graph: { nodes: graph.nodes.length, edges: graph.edges.length, proposed: graph.edges.filter((edge) => edge.verificationStatus === "proposed").length },
          audit: { total: auditIntegrity.count, today: auditEvents.filter((event) => event.createdAt.startsWith(today)).length, integrity: auditIntegrity.valid, blocked: auditEvents.filter((event) => /block|reject|deny|拦截|拒绝/i.test(event.action)).length },
        } });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/audit") {
        if (!requireRole("admin")) return;
        sendJson(response, 200, {
          events: platformStore?.listAuditEvents(workspaceId, url.searchParams.get("limit")) ?? [],
          integrity: platformStore ? platformStore.verifyAuditChain(workspaceId) : { valid: false, count: 0, unavailable: true },
        });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/audit/events") {
        if (!requireRole("viewer")) return;
        if (!platformStore) { sendJson(response, 409, { error: "Audit ledger is unavailable in this storage mode" }); return; }
        const event = safeUiAuditInput(await readJson(request), workspaceId);
        if (event.action === "audit.export" && !requireRole("admin")) return;
        const recorded = platformStore.appendAudit(workspaceId, { actorUserId: session.user.id, ...event });
        sendJson(response, 201, { event: recorded });
        return;
      }

      if (request.method === "GET" && url.pathname === "/api/admin/operations") {
        if (!requireRole("admin")) return;
        const [backups, semanticEnhancement] = await Promise.all([backupService ? backupService.listBackups() : [], semanticaClient.status()]);
        sendJson(response, 200, {
          scanner: fileSecurityService.status(),
          storage: platformStore ? { adapter: "sqlite", integrity: platformStore.integrityCheck().valid ? "ok" : "failed", backupsEnabled: Boolean(backupService) } : { adapter: "atomic-json", integrity: "not-applicable", backupsEnabled: false },
          audit: platformStore ? platformStore.verifyAuditChain(workspaceId) : { valid: false, count: 0, unavailable: true },
          backups,
          jobs: analysisService.list(workspaceId, 20),
          semanticEnhancement,
          semanticReviews: platformStore?.listSemanticReviews(workspaceId, { limit: 100 }) ?? [],
        });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/admin/semantic/reviews") {
        if (!requireRole("admin")) return;
        if (!platformStore) { sendJson(response, 409, { error: "Semantic review storage is unavailable in this mode" }); return; }
        const requestedStatus = String(url.searchParams.get("status") || "").trim();
        if (requestedStatus && !["pending", "confirmed", "dismissed"].includes(requestedStatus)) throw new Error("Semantic review status is invalid");
        sendJson(response, 200, { reviews: platformStore.listSemanticReviews(workspaceId, { status: requestedStatus, limit: url.searchParams.get("limit") }) });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/admin/semantic/enrich") {
        if (!requireRole("admin")) return;
        const semanticIdentity = `${workspaceId}:${session.user.id}`;
        if (!allowWindow(semanticRateWindows, semanticIdentity, semanticRateLimit)) {
          response.setHeader("retry-after", "60");
          sendJson(response, 429, { error: "Too many semantic checks; retry in 60 seconds" });
          return;
        }
        const assets = await publicationRegistry.listAssets(workspaceId, { role: session.user.role });
        const rawResult = await semanticaClient.enrich(assets);
        const reviews = platformStore ? platformStore.upsertSemanticReviews(workspaceId, rawResult) : [];
        const result = attachSemanticReviews(rawResult, reviews);
        appendAudit(session, "semantic.check", "published_asset_set", workspaceId, { checkedAssets: result.checkedAssets, duplicateCandidates: result.duplicates.length, conflicts: result.conflicts.length, engine: result.engine, version: result.version, formalKnowledgeMutation: false });
        sendJson(response, 200, { result, reviews });
        return;
      }
      const semanticReviewDecisionMatch = request.method === "POST" && url.pathname.match(/^\/api\/admin\/semantic\/reviews\/(SEMREV-[A-F0-9]{24})\/decision$/);
      if (semanticReviewDecisionMatch) {
        if (!requireRole("admin")) return;
        if (!platformStore) { sendJson(response, 409, { error: "Semantic review storage is unavailable in this mode" }); return; }
        const input = safeSemanticReviewDecision(await readJson(request));
        const review = platformStore.decideSemanticReview(workspaceId, semanticReviewDecisionMatch[1], { ...input, reviewerUserId: session.user.id }, {
          audit: {
            actorUserId: session.user.id,
            action: input.decision === "confirmed" ? "semantic.review_confirm" : "semantic.review_dismiss",
            objectType: "semantic_review",
          },
        });
        sendJson(response, 200, { review });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/admin/members") {
        if (!requireRole("admin")) return;
        sendJson(response, 200, { members: platformStore?.listMembers(workspaceId) ?? [], invitations: platformStore?.listInvitations(workspaceId) ?? [] });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/admin/invitations") {
        if (!requireRole("admin")) return;
        const input = await readJson(request);
        const ttlMs = input.expires === "24h" ? 24 * 60 * 60_000 : input.expires === "30d" ? 30 * 24 * 60 * 60_000 : 7 * 24 * 60 * 60_000;
        const invitation = authService.createInvitation({ workspaceId, email: input.email, name: input.name, role: input.role, invitedBy: session.user.id, ttlMs });
        appendAudit(session, "member.invitation_create", "invitation", invitation.invitation.id, { email: invitation.invitation.email, role: invitation.invitation.role, expiresAt: invitation.invitation.expiresAt });
        sendJson(response, 201, invitation);
        return;
      }
      const revokeInvitationMatch = request.method === "DELETE" && url.pathname.match(/^\/api\/admin\/invitations\/(INV-[A-Za-z0-9-]+)$/);
      if (revokeInvitationMatch) {
        if (!requireRole("admin")) return;
        const invitation = platformStore.revokeInvitation(workspaceId, revokeInvitationMatch[1]);
        if (!invitation) { sendJson(response, 404, { error: "Pending invitation not found" }); return; }
        appendAudit(session, "member.invitation_revoke", "invitation", invitation.id, { email: invitation.email });
        sendJson(response, 200, { invitation });
        return;
      }
      const memberUpdateMatch = request.method === "PATCH" && url.pathname.match(/^\/api\/admin\/members\/(USR-[A-Za-z0-9-]+)$/);
      if (memberUpdateMatch) {
        if (!requireRole("admin")) return;
        if (memberUpdateMatch[1] === session.user.id) { sendJson(response, 409, { error: "Use another workspace owner to change your own membership" }); return; }
        const input = await readJson(request);
        if (!['admin', 'editor', 'viewer'].includes(input.role) || !['active', 'disabled'].includes(input.status)) throw Object.assign(new Error("A valid member role and status are required"), { code: "INVALID_ROLE" });
        const member = platformStore.updateMember(workspaceId, memberUpdateMatch[1], { role: input.role, disabled: input.status === "disabled" });
        if (!member) { sendJson(response, 404, { error: "Workspace member not found" }); return; }
        appendAudit(session, "member.update", "user", member.id, { role: member.role, status: member.status });
        sendJson(response, 200, { member });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/admin/backups") {
        if (!requireRole("admin")) return;
        if (!backupService) { sendJson(response, 409, { error: "Verified SQLite backups are not available in this storage mode" }); return; }
        const backup = await backupService.createBackup({ createdBy: session.user.id });
        appendAudit(session, "backup.create", "backup", backup.id, { size: backup.size, sha256: backup.sha256, integrity: backup.integrity });
        sendJson(response, 201, { backup });
        return;
      }
      const verifyBackupMatch = request.method === "POST" && url.pathname.match(/^\/api\/admin\/backups\/(BKP-\d{8}T\d{6}-[a-f0-9]{12})\/verify$/);
      if (verifyBackupMatch) {
        if (!requireRole("admin")) return;
        if (!backupService) { sendJson(response, 409, { error: "Verified SQLite backups are not available in this storage mode" }); return; }
        const backup = await backupService.verifyBackup(verifyBackupMatch[1]);
        appendAudit(session, "backup.verify", "backup", backup.id, { sha256: backup.sha256, integrity: backup.integrity });
        sendJson(response, 200, { backup });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/shares") {
        if (!requireRole("editor")) return;
        sendJson(response, 200, { shares: shareService ? shareService.list(workspaceId) : [] });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/shares") {
        if (!requireRole("editor")) return;
        if (!shareService) { sendJson(response, 409, { error: "Secure sharing is unavailable in this storage mode" }); return; }
        const input = await readJson(request);
        const created = shareService.create({ workspaceId, assetId: input.assetId, recipientEmail: input.recipient, expires: input.expires, createdBy: session.user.id });
        appendAudit(session, "share.create", "secure_share", created.share.id, { assetId: created.share.assetId, recipient: created.share.recipientEmail, scope: created.share.scope, expiresAt: created.share.expiresAt });
        sendJson(response, 201, created);
        return;
      }
      const revokeShareMatch = request.method === "DELETE" && url.pathname.match(/^\/api\/shares\/(SHR-[A-Za-z0-9-]+)$/);
      if (revokeShareMatch) {
        if (!requireRole("editor")) return;
        const share = shareService?.revoke(workspaceId, revokeShareMatch[1]);
        if (!share) { sendJson(response, 404, { error: "Active secure share not found" }); return; }
        appendAudit(session, "share.revoke", "secure_share", share.id, { assetId: share.assetId, recipient: share.recipientEmail });
        sendJson(response, 200, { share });
        return;
      }

      if (request.method === "POST" && url.pathname === "/api/analysis") {
        if (!requireRole("editor")) return;
        const identity = authRequired ? `${workspaceId}:${session.user.id}` : request.socket.remoteAddress || "unknown";
        if (!allowWindow(rateWindows, identity, analysisRateLimit)) {
          response.setHeader("retry-after", "60");
          sendJson(response, 429, { error: "Too many analysis requests; retry in 60 seconds" });
          return;
        }
        const input = await parseMultipart(request);
        const job = await analysisService.submit(input.file, { expectedCategory: input.expectedCategory, workspaceId, actorUserId: session.user.id });
        appendAudit(session, "analysis.submit", "analysis_job", job.id, { documentName: job.document.name, documentSha256: job.document.sha256 });
        sendJson(response, 202, { job });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/agent/tasks") {
        if (!requireRole("viewer")) return;
        if (!agentService) { sendJson(response, 409, { error: "Persistent IP task agent is unavailable in this storage mode" }); return; }
        const identity = authRequired ? `${workspaceId}:${session.user.id}` : request.socket.remoteAddress || "unknown";
        if (!allowWindow(agentRateWindows, identity, agentRateLimit)) {
          response.setHeader("retry-after", "60");
          sendJson(response, 429, { error: "Too many Agent tasks; retry in 60 seconds" });
          return;
        }
        const task = await agentService.submit(await readJson(request), { workspaceId, userId: session.user.id, role: session.user.role });
        sendJson(response, task.state === "blocked" ? 200 : 202, { task });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/agent/tasks") {
        if (!requireRole("viewer")) return;
        if (!agentService) { sendJson(response, 200, { tasks: [] }); return; }
        sendJson(response, 200, { tasks: agentService.list({ workspaceId, userId: session.user.id }, url.searchParams.get("limit")) });
        return;
      }
      const agentTaskMatch = request.method === "GET" && url.pathname.match(/^\/api\/agent\/tasks\/(AGT-[A-Za-z0-9-]{1,100})$/);
      if (agentTaskMatch) {
        if (!requireRole("viewer")) return;
        const task = agentService?.get(agentTaskMatch[1], { workspaceId, userId: session.user.id });
        sendJson(response, task ? 200 : 404, task ? { task } : { error: "Agent task not found" });
        return;
      }
      const agentCancelMatch = request.method === "POST" && url.pathname.match(/^\/api\/agent\/tasks\/(AGT-[A-Za-z0-9-]{1,100})\/cancel$/);
      if (agentCancelMatch) {
        if (!requireRole("viewer")) return;
        const task = agentService?.cancel(agentCancelMatch[1], { workspaceId, userId: session.user.id });
        sendJson(response, task ? 200 : 404, task ? { task } : { error: "Agent task not found" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/analysis") {
        if (!requireRole("editor")) return;
        sendJson(response, 200, { jobs: analysisService.list(workspaceId) });
        return;
      }
      const retryMatch = request.method === "POST" && url.pathname.match(/^\/api\/analysis\/(JOB-REAL-[A-Za-z0-9-]+)\/retry$/);
      if (retryMatch) {
        if (!requireRole("editor")) return;
        const job = await analysisService.retry(retryMatch[1], workspaceId);
        if (!job) { sendJson(response, 404, { error: "Analysis job not found" }); return; }
        appendAudit(session, "analysis.retry", "analysis_job", job.id, {});
        sendJson(response, 202, { job });
        return;
      }
      const publishMatch = request.method === "POST" && url.pathname.match(/^\/api\/analysis\/(JOB-REAL-[A-Za-z0-9-]+)\/publish$/);
      if (publishMatch) {
        if (!requireRole("editor")) return;
        const job = analysisService.get(publishMatch[1], workspaceId);
        if (!job) { sendJson(response, 404, { error: "Analysis job not found" }); return; }
        if (job.state !== "complete") { sendJson(response, 409, { error: "Analysis must complete before publication" }); return; }
        const metadata = Number(request.headers["content-length"] ?? 0) > 0 ? await readJson(request) : {};
        const before = (await publicationRegistry.listAssets(workspaceId)).some((asset) => asset.sourceJobId === job.id);
        const publication = await publicationRegistry.publish(job, { owner: String(metadata.owner ?? "待确权").slice(0, 80), sensitivity: String(metadata.sensitivity ?? "待复核").slice(0, 40) }, workspaceId);
        appendAudit(session, "publication.publish", "publication", publication.publicationId, { sourceJobId: job.id, assetCount: publication.assets.length });
        sendJson(response, before ? 200 : 201, { publication });
        return;
      }
      const jobMatch = request.method === "GET" && url.pathname.match(/^\/api\/analysis\/(JOB-REAL-[A-Za-z0-9-]+)$/);
      if (jobMatch) {
        if (!requireRole("editor")) return;
        const job = analysisService.get(jobMatch[1], workspaceId);
        sendJson(response, job ? 200 : 404, job ? { job } : { error: "Analysis job not found" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/assets/graph") {
        if (!requireRole("viewer")) return;
        const graph = await publicationRegistry.getAssetGraph(workspaceId, {
          role: session.user.role,
          includeProposed: url.searchParams.get("includeProposed") === "true",
          types: queryList(url, "type"),
          relationTypes: queryList(url, "relationType"),
          rootAssetId: url.searchParams.get("root") ?? "",
          depth: url.searchParams.get("depth"),
          limit: url.searchParams.get("limit"),
          edgeLimit: url.searchParams.get("edgeLimit"),
        });
        sendJson(response, 200, { graph });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/assets/search") {
        if (!requireRole("viewer")) return;
        const search = await publicationRegistry.searchAssetGraph(url.searchParams.get("q") ?? "", workspaceId, {
          role: session.user.role,
          depth: url.searchParams.get("depth"),
          limit: url.searchParams.get("limit"),
        });
        sendJson(response, 200, search);
        return;
      }
      const neighborhoodMatch = request.method === "GET" && url.pathname.match(/^\/api\/assets\/(IP-[A-Za-z0-9-]{3,96})\/neighborhood$/);
      if (neighborhoodMatch) {
        if (!requireRole("viewer")) return;
        const graph = await publicationRegistry.getAssetGraph(workspaceId, {
          role: session.user.role,
          includeProposed: url.searchParams.get("includeProposed") === "true",
          rootAssetId: neighborhoodMatch[1],
          depth: url.searchParams.get("depth") ?? 1,
          limit: url.searchParams.get("limit"),
          edgeLimit: url.searchParams.get("edgeLimit"),
        });
        sendJson(response, graph.meta.rootUnavailable ? 404 : 200, graph.meta.rootUnavailable ? { error: "Asset not found" } : { graph });
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/relationships") {
        if (!requireRole("editor")) return;
        const input = safeRelationshipInput(await readJson(request));
        const relationship = await publicationRegistry.createAssetRelationship(workspaceId, { ...input, origin: "manual", createdBy: session.user.id }, { audit: { actorUserId: session.user.id, action: "relationship.create", objectType: "asset_relationship", detail: { sourceAssetId: input.sourceAssetId, targetAssetId: input.targetAssetId, relationType: input.relationType, evidenceCount: input.evidenceIds.length } } });
        sendJson(response, 201, { relationship });
        return;
      }
      const relationshipStatusMatch = request.method === "POST" && url.pathname.match(/^\/api\/relationships\/(REL-[A-Za-z0-9-]{8,96})\/(confirm|reject)$/);
      if (relationshipStatusMatch) {
        if (!requireRole("editor")) return;
        const nextStatus = relationshipStatusMatch[2] === "confirm" ? "confirmed" : "rejected";
        const relationship = await publicationRegistry.updateAssetRelationshipStatus(workspaceId, relationshipStatusMatch[1], nextStatus, { audit: { actorUserId: session.user.id, action: `relationship.${relationshipStatusMatch[2]}`, objectType: "asset_relationship", detail: { verificationStatus: nextStatus } } });
        sendJson(response, 200, { relationship });
        return;
      }
      const relationshipMatch = request.method === "GET" && url.pathname.match(/^\/api\/relationships\/(REL-[A-Za-z0-9-]{8,96})$/);
      if (relationshipMatch) {
        if (!requireRole("viewer")) return;
        const relationship = await publicationRegistry.getAssetRelationship(workspaceId, relationshipMatch[1], { role: session.user.role });
        sendJson(response, relationship ? 200 : 404, relationship ? { relationship } : { error: "Asset relationship not found" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/assets") {
        if (!requireRole("viewer")) return;
        sendJson(response, 200, { assets: await publicationRegistry.listAssets(workspaceId, { role: session.user.role }) });
        return;
      }
      if (request.method === "PATCH" && url.pathname === "/api/assets/metadata") {
        if (!requireRole("editor")) return;
        const input = await readJson(request);
        const assetIds = [...new Set((Array.isArray(input.assetIds) ? input.assetIds : []).map((assetId) => String(assetId || "").trim()).filter(Boolean))];
        if (!assetIds.length || assetIds.length > 50 || !assetIds.every((assetId) => /^IP-[A-Za-z0-9-]{3,96}$/.test(assetId))) throw Object.assign(new Error("Between 1 and 50 valid assets are required"), { code: "INVALID_ASSET_BATCH" });
        const owner = String(input.owner || "").slice(0, 80);
        const sensitivity = String(input.sensitivity || "").slice(0, 40);
        const assets = await publicationRegistry.updateAssetMetadataBatch(workspaceId, assetIds, { owner, sensitivity }, {
          role: session.user.role,
          audit: { actorUserId: session.user.id, action: "asset.metadata_batch_update", objectType: "ip_asset_batch", objectId: `${assetIds.length}-assets`, detail: { count: assetIds.length, owner, sensitivity } },
        });
        sendJson(response, 200, { assets });
        return;
      }
      const assetMetadataMatch = request.method === "PATCH" && url.pathname.match(/^\/api\/assets\/(IP-[A-Za-z0-9-]{3,96})\/metadata$/);
      if (assetMetadataMatch) {
        if (!requireRole("editor")) return;
        const input = await readJson(request);
        const owner = String(input.owner || "").slice(0, 80);
        const sensitivity = String(input.sensitivity || "").slice(0, 40);
        const asset = await publicationRegistry.updateAssetMetadata(workspaceId, assetMetadataMatch[1], { owner, sensitivity }, {
          role: session.user.role,
          audit: { actorUserId: session.user.id, action: "asset.metadata_update", objectType: "ip_asset", detail: { owner, sensitivity } },
        });
        sendJson(response, asset ? 200 : 404, asset ? { asset } : { error: "Asset not found" });
        return;
      }
      const assetMatch = request.method === "GET" && url.pathname.match(/^\/api\/assets\/(IP-[A-Za-z0-9-]{3,96})$/);
      if (assetMatch) {
        if (!requireRole("viewer")) return;
        const asset = await publicationRegistry.getAsset(workspaceId, assetMatch[1], { role: session.user.role });
        sendJson(response, asset ? 200 : 404, asset ? { asset } : { error: "Asset not found" });
        return;
      }
      const wikiVersionsMatch = request.method === "GET" && url.pathname.match(/^\/api\/wiki\/(IP-REAL-[A-F0-9]+)\/versions$/);
      if (wikiVersionsMatch) {
        if (!requireRole("viewer")) return;
        const versions = await publicationRegistry.listWikiVersions(workspaceId, wikiVersionsMatch[1], { role: session.user.role });
        sendJson(response, versions.length ? 200 : 404, versions.length ? { versions } : { error: "Wiki not found" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/wiki/reviews") {
        if (!requireRole("editor")) return;
        const status = url.searchParams.get("status") || "";
        sendJson(response, 200, { reviews: await publicationRegistry.listWikiReviews(workspaceId, { status }) });
        return;
      }
      const wikiReviewSubmitMatch = request.method === "POST" && url.pathname.match(/^\/api\/wiki\/(IP-REAL-[A-F0-9]+)\/reviews$/);
      if (wikiReviewSubmitMatch) {
        if (!requireRole("editor")) return;
        const input = safeWikiInput(await readJson(request));
        const review = await publicationRegistry.submitWikiReview(workspaceId, wikiReviewSubmitMatch[1], { ...input, submittedByUserId: session.user.id });
        if (!review) { sendJson(response, 404, { error: "Wiki not found" }); return; }
        appendAudit(session, "wiki.review_submit", "wiki_review", review.id, { assetId: review.assetId, baseVersion: review.baseVersion, changeNote: review.changeNote });
        sendJson(response, 201, { review });
        return;
      }
      const wikiReviewDecisionMatch = request.method === "POST" && url.pathname.match(/^\/api\/wiki\/reviews\/(WREV-[A-Za-z0-9-]+)\/decision$/);
      if (wikiReviewDecisionMatch) {
        if (!requireRole("admin")) return;
        const input = safeWikiReviewDecision(await readJson(request));
        const result = await publicationRegistry.decideWikiReview(workspaceId, wikiReviewDecisionMatch[1], { ...input, reviewerUserId: session.user.id });
        appendAudit(session, input.decision === "approved" ? "wiki.review_approve" : "wiki.review_reject", "wiki_review", result.review.id, { assetId: result.review.assetId, baseVersion: result.review.baseVersion, version: result.wiki?.version || null, reviewNote: input.reviewNote });
        sendJson(response, 200, result);
        return;
      }
      const wikiEditMatch = request.method === "PATCH" && url.pathname.match(/^\/api\/wiki\/(IP-REAL-[A-F0-9]+)$/);
      if (wikiEditMatch) {
        if (!requireRole("admin")) return;
        const input = safeWikiInput(await readJson(request));
        const wiki = await publicationRegistry.updateWiki(workspaceId, wikiEditMatch[1], { ...input, editorUserId: session.user.id });
        if (!wiki) { sendJson(response, 404, { error: "Wiki not found" }); return; }
        appendAudit(session, "wiki.update", "wiki", wikiEditMatch[1], { version: wiki.version, changeNote: input.changeNote });
        sendJson(response, 200, { wiki });
        return;
      }
      const wikiMatch = request.method === "GET" && url.pathname.match(/^\/api\/wiki\/(IP-REAL-[A-F0-9]+)$/);
      if (wikiMatch) {
        if (!requireRole("viewer")) return;
        const wiki = await publicationRegistry.getWiki(workspaceId, wikiMatch[1], { role: session.user.role });
        sendJson(response, wiki ? 200 : 404, wiki ? { wiki } : { error: "Wiki not found" });
        return;
      }
      const evidenceMatch = request.method === "GET" && url.pathname.match(/^\/api\/evidence\/(EV-[A-F0-9]+)$/);
      if (evidenceMatch) {
        if (!requireRole("viewer")) return;
        const evidence = await publicationRegistry.getEvidence(workspaceId, evidenceMatch[1], { role: session.user.role });
        sendJson(response, evidence ? 200 : 404, evidence ? { evidence } : { error: "Evidence not found" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/search") {
        if (!requireRole("viewer")) return;
        sendJson(response, 200, { results: await publicationRegistry.search(url.searchParams.get("q") ?? "", workspaceId, { role: session.user.role }) });
        return;
      }
      if (["POST", "PUT", "PATCH", "DELETE"].includes(request.method)) {
        sendJson(response, 405, { error: "Method not allowed" });
        return;
      }
      await serveStatic(response, url.pathname, distRoot);
    } catch (error) {
      const safeMessage = String(error?.message || "Gateway request failed").replace(/Bearer\s+[^\s]+/gi, "Bearer [redacted]").replace(/https?:\/\/\S+/gi, "[redacted-url]").slice(0, 220);
      const status = error?.code === "INVALID_CREDENTIALS" ? 401 : ["SEMANTICA_DISABLED", "SEMANTICA_UNAVAILABLE", "SEMANTICA_TIMEOUT", "SEMANTICA_PROCESS_FAILED"].includes(error?.code) ? 503 : ["VERSION_CONFLICT", "NO_CHANGES", "REVIEW_PENDING", "REVIEW_DECIDED", "SEMANTIC_REVIEW_DECIDED", "NOT_RETRYABLE", "BACKUP_IN_PROGRESS", "BACKUP_INTEGRITY_FAILED", "INVITATION_CONFLICT", "OWNER_PROTECTED", "DUPLICATE_RELATIONSHIP", "INVALID_RELATION_TRANSITION", "STORAGE_UNAVAILABLE"].includes(error?.code) ? 409 : error?.code === "UPLOAD_UNAVAILABLE" ? 410 : ["ENOENT", "NOT_FOUND", "INVITATION_UNAVAILABLE", "SHARE_UNAVAILABLE"].includes(error?.code) ? 404 : 400;
      if (request.url?.startsWith("/api/")) sendJson(response, status, { error: safeMessage, ...(error?.currentVersion ? { currentVersion: error.currentVersion } : {}) });
      else {
        applySecurityHeaders(response, { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" });
        response.writeHead(status === 404 ? 404 : 500);
        response.end(status === 404 ? "Not found" : "Gateway request failed");
      }
    }
  });

  return {
    server,
    analysisService,
    agentService,
    publicationRegistry,
    fileSecurityService,
    backupService,
    semanticaClient,
    shareService,
    platformStore,
    authService,
    async start(port = options.port ?? 0) {
      await new Promise((resolve, reject) => { server.once("error", reject); server.listen(port, host, resolve); });
      const address = server.address();
      return `http://${host}:${address.port}`;
    },
    async stop() {
      if (server.listening) await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
      if (ownsPlatformStore) platformStore.close();
    },
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const production = process.env.NODE_ENV === "production";
  const bootstrapPassword = process.env.INTELIFAR_BOOTSTRAP_PASSWORD;
  if (production && !bootstrapPassword) throw new Error("Production startup requires INTELIFAR_BOOTSTRAP_PASSWORD");
  const gateway = await createRealAnalysisServer({
    host: process.env.HOST ?? "127.0.0.1",
    databasePath: process.env.INTELIFAR_DATABASE_PATH ?? path.resolve(process.cwd(), ".runtime", "intelifar.sqlite"),
    migrateLegacyPublications: true,
    auth: bootstrapPassword ? {
      required: true,
      secureCookies: process.env.INTELIFAR_SECURE_COOKIES !== "false",
      workspaceId: process.env.INTELIFAR_WORKSPACE_ID ?? "WS-PRIMARY",
      workspaceName: process.env.INTELIFAR_WORKSPACE_NAME ?? "intelifar 工作空间",
      email: process.env.INTELIFAR_BOOTSTRAP_EMAIL ?? "owner@intelifar.local",
      password: bootstrapPassword,
      name: process.env.INTELIFAR_BOOTSTRAP_NAME ?? "空间所有者",
    } : { required: false },
    semantica: {
      enabled: process.env.INTELIFAR_SEMANTICA_ENABLED === "true",
      pythonPath: process.env.INTELIFAR_SEMANTICA_PYTHON,
      sourcePath: process.env.INTELIFAR_SEMANTICA_SOURCE_PATH ?? path.resolve(here, "../.runtime/semantica-src-v0.6.0"),
      bridgePath: process.env.INTELIFAR_SEMANTICA_BRIDGE_PATH ?? path.resolve(here, "../../integrations/semantica/bridge.py"),
      timeoutMs: Number(process.env.INTELIFAR_SEMANTICA_TIMEOUT_MS ?? 15_000),
    },
  });
  const url = await gateway.start(Number(process.env.PORT ?? 4388));
  process.stdout.write(`intelifar real analysis gateway listening on ${url}\n`);
}
