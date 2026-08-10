import { readFile, stat } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
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
const DEMO_SESSION = { user: { id: "USR-DEMO", email: "demo@intelifar.local", name: "林越", role: "owner" }, workspace: { id: "WS-DEMO", name: "澜图科技" }, mode: "demo" };

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
  if (platformStore && !authRequired) platformStore.ensureWorkspace({ id: defaultWorkspaceId, name: DEMO_SESSION.workspace.name });
  const fileSecurityService = options.fileSecurityService ?? createFileSecurityService({
    externalScanner: options.externalScanner,
    clamAvPath: options.clamAvPath,
    requireExternal: options.requireExternalScanner,
  });
  const analysisService = options.analysisService ?? createAnalysisService({ mineruClient, deepseekClient, fileSecurityService, jobStore: platformStore, uploadRoot: options.uploadRoot, defaultWorkspaceId });
  const publicationRegistry = options.publicationRegistry ?? createPublicationRegistry({ rootDir: options.registryRoot, store: platformStore, defaultWorkspaceId });
  const shareService = options.shareService ?? (platformStore ? createShareService({ store: platformStore }) : null);
  const backupService = options.backupService ?? (platformStore ? createBackupService({ store: platformStore, backupRoot: options.backupRoot, retention: options.backupRetention }) : null);
  const distRoot = path.resolve(options.distRoot ?? DEFAULT_DIST);
  const host = options.host ?? "127.0.0.1";
  const analysisRateLimit = Math.max(1, Number(options.analysisRateLimit ?? 8));
  const loginRateLimit = Math.max(1, Number(options.loginRateLimit ?? 6));
  const rateWindows = new Map();
  const loginRateWindows = new Map();
  const publicRateWindows = new Map();
  const publicAccessRateLimit = Math.max(1, Number(options.publicAccessRateLimit ?? 20));

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
    return authRequired ? authService.getSessionFromRequest(request) : DEMO_SESSION;
  }

  function appendAudit(session, action, objectType, objectId, detail = {}) {
    if (!platformStore || session.mode === "demo") return;
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
        sendJson(response, 200, {
          status: "ok",
          mode: "real",
          providers: { mineru: "configured", deepseek: "configured" },
          model: config.deepseekModel,
          auth: { required: authRequired, mode: authRequired ? "local-session" : "loopback-demo" },
          storage: { adapter: platformStore ? "sqlite" : "atomic-json", durableJobs: Boolean(platformStore), wikiVersions: Boolean(platformStore), verifiedBackups: Boolean(backupService), memberLifecycle: Boolean(platformStore), secureShares: Boolean(shareService) },
          fileSecurity: fileSecurityService.status(),
          dataBoundary: { gateway: "local", externalProcessors: ["MinerU", "DeepSeek"], disclosure: "Documents are sent to MinerU for parsing; bounded parsed text is sent to DeepSeek for structured analysis." },
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

      if (request.method === "GET" && url.pathname === "/api/admin/operations") {
        if (!requireRole("admin")) return;
        const backups = backupService ? await backupService.listBackups() : [];
        sendJson(response, 200, {
          scanner: fileSecurityService.status(),
          storage: platformStore ? { adapter: "sqlite", integrity: platformStore.integrityCheck().valid ? "ok" : "failed", backupsEnabled: Boolean(backupService) } : { adapter: "atomic-json", integrity: "not-applicable", backupsEnabled: false },
          audit: platformStore ? platformStore.verifyAuditChain(workspaceId) : { valid: false, count: 0, unavailable: true },
          backups,
          jobs: analysisService.list(workspaceId, 20),
        });
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
        const backup = await backupService.createBackup({ createdBy: authRequired ? session.user.id : null });
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
        const job = await analysisService.submit(input.file, { expectedCategory: input.expectedCategory, workspaceId, actorUserId: authRequired ? session.user.id : null });
        appendAudit(session, "analysis.submit", "analysis_job", job.id, { documentName: job.document.name, documentSha256: job.document.sha256 });
        sendJson(response, 202, { job });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/analysis") {
        if (!requireRole("viewer")) return;
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
        if (!requireRole("viewer")) return;
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
        const relationship = await publicationRegistry.createAssetRelationship(workspaceId, { ...input, origin: "manual", createdBy: authRequired ? session.user.id : null }, authRequired ? { audit: { actorUserId: session.user.id, action: "relationship.create", objectType: "asset_relationship", detail: { sourceAssetId: input.sourceAssetId, targetAssetId: input.targetAssetId, relationType: input.relationType, evidenceCount: input.evidenceIds.length } } } : {});
        sendJson(response, 201, { relationship });
        return;
      }
      const relationshipStatusMatch = request.method === "POST" && url.pathname.match(/^\/api\/relationships\/(REL-[A-Za-z0-9-]{8,96})\/(confirm|reject)$/);
      if (relationshipStatusMatch) {
        if (!requireRole("editor")) return;
        const nextStatus = relationshipStatusMatch[2] === "confirm" ? "confirmed" : "rejected";
        const relationship = await publicationRegistry.updateAssetRelationshipStatus(workspaceId, relationshipStatusMatch[1], nextStatus, authRequired ? { audit: { actorUserId: session.user.id, action: `relationship.${relationshipStatusMatch[2]}`, objectType: "asset_relationship", detail: { verificationStatus: nextStatus } } } : {});
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
      const wikiEditMatch = request.method === "PATCH" && url.pathname.match(/^\/api\/wiki\/(IP-REAL-[A-F0-9]+)$/);
      if (wikiEditMatch) {
        if (!requireRole("editor")) return;
        const input = safeWikiInput(await readJson(request));
        const wiki = await publicationRegistry.updateWiki(workspaceId, wikiEditMatch[1], { ...input, editorUserId: authRequired ? session.user.id : null });
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
      const status = error?.code === "INVALID_CREDENTIALS" ? 401 : ["VERSION_CONFLICT", "NOT_RETRYABLE", "BACKUP_IN_PROGRESS", "BACKUP_INTEGRITY_FAILED", "INVITATION_CONFLICT", "OWNER_PROTECTED", "DUPLICATE_RELATIONSHIP", "INVALID_RELATION_TRANSITION", "STORAGE_UNAVAILABLE"].includes(error?.code) ? 409 : error?.code === "UPLOAD_UNAVAILABLE" ? 410 : ["ENOENT", "NOT_FOUND", "INVITATION_UNAVAILABLE", "SHARE_UNAVAILABLE"].includes(error?.code) ? 404 : 400;
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
    publicationRegistry,
    fileSecurityService,
    backupService,
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
    databasePath: bootstrapPassword ? process.env.INTELIFAR_DATABASE_PATH ?? path.resolve(process.cwd(), ".runtime", "intelifar.sqlite") : undefined,
    auth: bootstrapPassword ? {
      required: true,
      secureCookies: process.env.INTELIFAR_SECURE_COOKIES !== "false",
      workspaceId: process.env.INTELIFAR_WORKSPACE_ID ?? "WS-PRIMARY",
      workspaceName: process.env.INTELIFAR_WORKSPACE_NAME ?? "intelifar 工作空间",
      email: process.env.INTELIFAR_BOOTSTRAP_EMAIL ?? "owner@intelifar.local",
      password: bootstrapPassword,
      name: process.env.INTELIFAR_BOOTSTRAP_NAME ?? "空间所有者",
    } : { required: false },
  });
  const url = await gateway.start(Number(process.env.PORT ?? 4322));
  process.stdout.write(`intelifar real analysis gateway listening on ${url}\n`);
}
