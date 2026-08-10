# intelifar IP Wiki Architecture

## Runtime shape

```text
Astro product shell
  -> same-origin Node analysis gateway (current real adapter)
       -> runtime-only credential loader
       -> local scrypt account + opaque HttpOnly session adapter
       -> one-time member invitation + revocable role lifecycle
       -> MinerU v4 upload / polling / ZIP Markdown adapter
       -> DeepSeek JSON Output IP analysis adapter
       -> double-credential redacted Wiki share adapter
       -> SQLite workspace, job, publication, Wiki version, member, share and audit adapter
       -> retained uploads for explicit interrupted-job retry
  -> deterministic domain state (retained offline acceptance adapter)
  -> Reasonix controller / agent runtime (preserved upstream kernel)
  -> remaining production connector boundary
       -> enterprise IdP, PostgreSQL/object storage, distributed queue and SIEM
```

The web surface is transport-agnostic and calls only same-origin APIs. In SMB mode the gateway verifies an opaque session, enforces a four-level workspace role, validates file signatures and provider-controlled URLs, streams no secrets to the browser, and normalizes all model output before it reaches the DOM. SQLite supplies transactional single-node state and explicit restart recovery; the deterministic adapter remains available for offline CI. A multi-instance rollout replaces the storage and identity adapters without duplicating business behavior inside individual views.

### Real provider flow

1. `POST /api/analysis` accepts a bounded multipart document and returns `JOB-REAL-*`.
2. MinerU v4 creates a signed upload URL, receives the binary, and is polled until `done`.
3. The gateway downloads the official result archive, reads `full.md` in memory, and calculates its SHA-256 digest.
4. A bounded Markdown excerpt is sent to `deepseek-v4-flash` using JSON Output and a fixed IP analysis schema.
5. `GET /api/analysis/:id` returns progress or normalized assets, risks, Wiki sections, usage metadata, and source quotations.

Windows development can set `INTELIFAR_HTTPS_PROXY`; the current workspace falls back to the detected local WinHTTP proxy for MinerU CDN downloads after direct connection failure. Production must configure proxy routing explicitly.

## Core contracts

### Intake

Input: document binary, declared/automatic category, owner department, initial sensitivity. Output: immutable document ID and processing job ID. Upload transport must support chunking, checksum verification, malware scanning, and resumable writes.

### Analysis event stream

Stages are `parse`, `classify`, `extract`, `verify`, and `wiki`. Every event includes job ID, stage, monotonic sequence, progress, trace ID, timestamp, and a user-safe status summary. Failures include a retry policy and never expose source content in logs.

### IP asset

Each schema field carries value, confidence, extractor version, evidence ID, document ID, page/content-block locator, content hash, and validation state. Asset versions are append-only; edits create a new version and audit event.

### Provenance

The lookup path is asset field -> evidence record -> immutable document version -> page/content block -> render coordinates. Access to redacted source requires explicit policy evaluation and creates a sensitive audit event before content is returned.

### Lifecycle and audit

Share and transfer operations require RBAC plus contextual ABAC evaluation. The SMB adapter implements external sharing as a high-entropy URL-fragment token plus a separately delivered access code; only their hashes are retained. Access defaults to a fixed redacted Wiki allowlist, explicit expiry, masked recipient watermarking, access counting and immediate revocation. Audit storage is append-only, hash chained, exportable, and independently verifiable. Recipient mailbox verification and automated email delivery remain deployment integrations, not implied application behavior.

## Security posture

The single-node SMB adapter requires HTTPS, encrypted disks, restricted filesystem permissions, automated SQLite backup/restore verification, malware scanning and an explicit MinerU/DeepSeek data policy before real confidential data is accepted. Larger enterprise services belong inside the organization's approved VPC or on-premises zone. OIDC/LDAP identity, service-to-service mTLS, per-tenant object prefixes, least-privilege service accounts, managed database/object storage, content-level authorization, and retention/erasure policy enforcement remain enterprise deployment responsibilities.

## Deployment path

1. Run the SQLite adapter as one application instance with one encrypted persistent volume; do not place two writers behind a load balancer.
2. Put the gateway behind HTTPS, keep secure cookies enabled, and inject bootstrap/provider secrets at runtime.
3. Schedule consistent SQLite backups and perform a restore drill before accepting customer data.
4. Add malware scanning and formal retention/deletion jobs before general availability.
5. For multi-instance deployment, replace SQLite and retained local uploads with PostgreSQL, object storage and a distributed worker queue.
6. Connect enterprise OIDC/SIEM through the existing identity and audit boundaries, then complete security, load, recovery and migration testing.
