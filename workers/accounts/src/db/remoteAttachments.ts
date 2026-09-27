import { generateToken, hashToken } from "../auth/crypto";

export interface RemoteAttachmentGrant {
  objectId: string;
  uploadTicket: string;
  downloadTicket: string;
  maxBytes: number;
  expiresAt: string;
}

export interface AuthorizedAttachment {
  objectId: string;
  userId: number;
  targetDeviceId: string;
  maxBytes: number;
  ciphertextBytes: number | null;
  ciphertextSha256: string | null;
  expiresAt: string;
}

interface AttachmentRow {
  object_id: string;
  user_id: number;
  target_device_id: string;
  max_bytes: number;
  ciphertext_bytes: number | null;
  ciphertext_sha256: string | null;
  expires_at: string;
}

function authorized(row: AttachmentRow): AuthorizedAttachment {
  return {
    objectId: row.object_id,
    userId: row.user_id,
    targetDeviceId: row.target_device_id,
    maxBytes: row.max_bytes,
    ciphertextBytes: row.ciphertext_bytes,
    ciphertextSha256: row.ciphertext_sha256,
    expiresAt: row.expires_at,
  };
}

export class RemoteAttachmentRepo {
  constructor(
    private readonly db: D1Database,
    private readonly pepper: string,
  ) {}

  async issue(input: {
    userId: number;
    targetDeviceId: string;
    maxBytes: number;
    ttlMs: number;
  }): Promise<RemoteAttachmentGrant> {
    const objectId = generateToken();
    const uploadTicket = generateToken();
    const downloadTicket = generateToken();
    const [uploadHash, downloadHash] = await Promise.all([
      hashToken(this.pepper, uploadTicket),
      hashToken(this.pepper, downloadTicket),
    ]);
    const now = new Date();
    const expiresAt = new Date(now.getTime() + input.ttlMs).toISOString();
    await this.db.prepare(
      `INSERT INTO remote_attachment_grants (
         object_id, user_id, target_device_id, upload_ticket_hash,
         download_ticket_hash, max_bytes, created_at, expires_at
       ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
    ).bind(
      objectId, input.userId, input.targetDeviceId, uploadHash, downloadHash,
      input.maxBytes, now.toISOString(), expiresAt,
    ).run();
    return { objectId, uploadTicket, downloadTicket, maxBytes: input.maxBytes, expiresAt };
  }

  async consumeUpload(input: {
    objectId: string;
    ticket: string;
    ciphertextBytes: number;
    ciphertextSha256: string;
  }): Promise<AuthorizedAttachment | null> {
    const ticketHash = await hashToken(this.pepper, input.ticket);
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `UPDATE remote_attachment_grants
       SET uploaded_at = ?1, ciphertext_bytes = ?2, ciphertext_sha256 = ?3
       WHERE object_id = ?4 AND upload_ticket_hash = ?5
         AND uploaded_at IS NULL AND expires_at > ?1 AND max_bytes >= ?2
         AND EXISTS (
           SELECT 1 FROM remote_devices d
           WHERE d.id = remote_attachment_grants.target_device_id
             AND d.user_id = remote_attachment_grants.user_id
             AND d.revoked_at IS NULL
         )
       RETURNING object_id, user_id, target_device_id, max_bytes,
                 ciphertext_bytes, ciphertext_sha256, expires_at`,
    ).bind(now, input.ciphertextBytes, input.ciphertextSha256, input.objectId, ticketHash)
      .first<AttachmentRow>();
    return row ? authorized(row) : null;
  }

  async authorizeDownload(objectId: string, ticket: string): Promise<AuthorizedAttachment | null> {
    const ticketHash = await hashToken(this.pepper, ticket);
    const now = new Date().toISOString();
    const row = await this.db.prepare(
      `SELECT object_id, user_id, target_device_id, max_bytes,
              ciphertext_bytes, ciphertext_sha256, expires_at
       FROM remote_attachment_grants
       WHERE object_id = ?1 AND download_ticket_hash = ?2
         AND uploaded_at IS NOT NULL AND expires_at > ?3
         AND EXISTS (
           SELECT 1 FROM remote_devices d
           WHERE d.id = remote_attachment_grants.target_device_id
             AND d.user_id = remote_attachment_grants.user_id
             AND d.revoked_at IS NULL
         )`,
    ).bind(objectId, ticketHash, now).first<AttachmentRow>();
    return row ? authorized(row) : null;
  }
}
