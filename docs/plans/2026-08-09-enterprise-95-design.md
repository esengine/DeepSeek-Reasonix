# intelifar Enterprise 95+ Design

## Objective and scorecard

The platform will be evaluated as an enterprise service, not as a static prototype. The fixed 100-point scorecard is: functional coverage 30, real-data closure 20, security and compliance 15, UX/accessibility/responsive quality 15, reliability and error handling 10, and automated evidence 10. The current baseline is 78/100: all deterministic tests pass, but live analysis results are not published into the asset registry or Wiki, live provenance is only quote/section level, Wiki search is inert, several controls are decorative, and the UI incorrectly claims that data never leaves the enterprise network while real mode calls MinerU and DeepSeek.

The target is at least 95/100 with no category below 85%. The product direction remains a governed enterprise knowledge workspace: operators ingest documents, review machine evidence, publish canonical assets, navigate a searchable Wiki, redact and share under policy, and preserve an auditable chain of custody. Obsidian-like backlinks and local graph navigation are useful interaction patterns, but free-form notes are not the system of record.

## Selected approach

Three approaches were considered. A visual-only pass is low risk but cannot close the enterprise data flow. A production database/SSO/queue rebuild would be architecturally complete but is too broad for this repository delivery. The selected approach is a vertical-slice reference implementation: durable local JSON publication storage with atomic writes, explicit publish APIs, stable evidence records, dynamic asset/Wiki rendering, truthful provider-boundary status, working search, keyboard-safe dialogs/drawers, clearer typography, and expanded browser/API tests. The storage interface remains replaceable by PostgreSQL/object storage in deployment.

## Data and interaction flow

MinerU produces Markdown; DeepSeek returns normalized document, assets, risks, Wiki sections, metrics, relationships, and verbatim source quotes. The analysis service attaches stable evidence identifiers, quote hashes, document hashes, parser task metadata, and honest location precision. A completed job remains a draft until an authorized operator publishes it. Publication writes an immutable version snapshot to the registry and exposes it through asset, Wiki, evidence, and search APIs. The browser renders published records instead of copying provider data into HTML fixtures.

The primary UI flow is upload → live parsing → structured review → publish → asset detail → dynamic Wiki → evidence drawer → audit. Publish has loading, success, duplicate, and failure states. Search covers asset ID, title, tags, summary, owner, and source document. Real evidence never invents a page number; it displays section-level precision unless MinerU supplied a verified page/block anchor.

## Enterprise safety and quality

Credentials stay server-side. Upload validation, bounded request bodies, provider URL allowlists, sanitized errors, CSP, method restrictions, and credential scans remain mandatory. Analysis creation receives a small in-memory rate limit. Runtime messaging distinguishes local gateway processing from authorized external processors and links those processors to visible health state. Published records are written atomically, exclude provider secrets and signed URLs, and use a configurable runtime directory excluded from source control.

The UI follows the existing intelifar precision-console direction while raising minimum readable sizes, preserving dense tables, adding visible focus, focus restoration, Escape closing, and drawer focus containment. Decorative actions are either wired or clearly labelled unavailable. Desktop and mobile critical paths receive browser verification, and final scoring is backed by unit, API, contract, offline E2E, live E2E, credential scan, dependency audit, and visual review artifacts.
