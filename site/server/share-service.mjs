import { createHash, randomBytes, randomUUID, timingSafeEqual } from "node:crypto";

const EXPIRY_MS = { "24h": 24 * 60 * 60_000, "7d": 7 * 24 * 60 * 60_000, "30d": 30 * 24 * 60 * 60_000 };

function sha256(value) {
  return createHash("sha256").update(String(value)).digest("hex");
}

function validEmail(value) {
  const email = String(value ?? "").trim().toLocaleLowerCase("en-US");
  return email.length <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ? email : "";
}

function maskedEmail(value) {
  const [local, domain] = String(value).split("@");
  const head = local.slice(0, Math.min(2, local.length));
  return `${head}${"*".repeat(Math.max(2, Math.min(6, local.length - head.length)))}@${domain}`;
}

function unavailable() {
  const error = new Error("Secure share is unavailable");
  error.code = "SHARE_UNAVAILABLE";
  return error;
}

export function createShareService(options) {
  const store = options?.store;
  if (!store?.createShare || !store?.findAsset) throw new Error("A platform store is required for secure sharing");
  const now = options.now ?? (() => new Date());

  function activeRecord(token) {
    const value = String(token ?? "");
    if (value.length < 32 || value.length > 128) return null;
    const record = store.getShareSecretRecordByTokenHash(sha256(value));
    if (!record || record.revokedAt || Date.parse(record.expiresAt) <= now().getTime()) return null;
    return record;
  }

  function inspect(token) {
    const record = activeRecord(token);
    if (!record) return null;
    const asset = store.findAsset(record.workspaceId, record.assetId);
    if (!asset) return null;
    return {
      shareId: record.id,
      recipient: maskedEmail(record.recipientEmail),
      scope: record.scope,
      expiresAt: record.expiresAt,
      documentTitle: asset.wiki?.title || asset.title,
      requiresAccessCode: true,
    };
  }

  return {
    create(input) {
      const workspaceId = String(input.workspaceId);
      const assetId = String(input.assetId ?? "");
      const recipientEmail = validEmail(input.recipientEmail);
      const expiryMs = EXPIRY_MS[input.expires];
      if (!recipientEmail) throw new Error("A valid recipient email is required");
      if (!expiryMs) throw new Error("Share expiry must be 24h, 7d, or 30d");
      if (!store.findAsset(workspaceId, assetId)) {
        const error = new Error("Share asset was not found in this workspace");
        error.code = "NOT_FOUND";
        throw error;
      }
      const token = randomBytes(32).toString("base64url");
      const accessCode = randomBytes(12).toString("base64url");
      const createdAt = now().toISOString();
      const share = store.createShare(workspaceId, {
        id: `SHR-${randomUUID()}`,
        assetId,
        recipientEmail,
        tokenHash: sha256(token),
        accessCodeHash: sha256(accessCode),
        createdBy: input.createdBy || null,
        createdAt,
        expiresAt: new Date(now().getTime() + expiryMs).toISOString(),
      });
      return { share, token, accessCode, sharePath: `/shared/#${token}` };
    },
    list(workspaceId) {
      return store.listShares(String(workspaceId));
    },
    revoke(workspaceId, id) {
      return store.revokeShare(String(workspaceId), String(id), now().toISOString());
    },
    inspect,
    access(input) {
      const record = activeRecord(input?.token);
      const suppliedHash = sha256(String(input?.accessCode ?? ""));
      const validCode = record && timingSafeEqual(Buffer.from(record.accessCodeHash, "hex"), Buffer.from(suppliedHash, "hex"));
      if (!record || !validCode) throw unavailable();
      const asset = store.findAsset(record.workspaceId, record.assetId);
      if (!asset) throw unavailable();
      const accessed = store.recordShareAccess(record.workspaceId, record.id, now().toISOString());
      store.appendAudit(record.workspaceId, {
        actorUserId: null,
        action: "share.access",
        objectType: "secure_share",
        objectId: record.id,
        detail: { scope: record.scope, recipient: maskedEmail(record.recipientEmail), accessCount: accessed.accessCount },
      });
      return {
        share: { id: record.id, scope: record.scope, expiresAt: record.expiresAt, recipient: maskedEmail(record.recipientEmail), accessCount: accessed.accessCount },
        wiki: {
          title: asset.wiki?.title || asset.title,
          version: asset.version,
          executiveSummary: asset.wiki?.executiveSummary || asset.summary || "",
          keyMechanism: asset.wiki?.keyMechanism || "",
          metrics: Array.isArray(asset.wiki?.metrics) ? asset.wiki.metrics : [],
          relationships: Array.isArray(asset.wiki?.relationships) ? asset.wiki.relationships : [],
        },
      };
    },
  };
}
