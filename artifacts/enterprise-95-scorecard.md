# intelifar Enterprise Acceptance Scorecard

- Date: 2026-08-09
- Baseline: 78 / 100
- Final repository acceptance score: **96 / 100**
- Gate: no critical or high product defect open; all automated acceptance commands pass

| Category | Weight | Baseline | Final | Evidence |
| --- | ---: | ---: | ---: | --- |
| Functional module coverage | 30 | 25 | 29 | Nine modules exercised; asset/Wiki/audit search, publishing, export, share, filters and focus states are wired |
| Real-data closure | 20 | 11 | 19 | MinerU → DeepSeek → review → publish → asset → Wiki → evidence completed with 4 assets and 8 quotes |
| Security and compliance | 15 | 11 | 14 | Same-origin gateway, server-only secrets, signature/size validation, provider URL allowlist, CSP, cross-origin write rejection, rate limiting and truthful data-boundary disclosure |
| UX, accessibility and responsive quality | 15 | 13 | 14 | Readable type scale, keyboard rows, focus trap/restoration, Escape close, Wiki focus mode, empty/loading/error states, 390px mobile verification |
| Reliability and error handling | 10 | 9 | 10 | Atomic publication snapshots, idempotent publishing, restart reload tests, safe provider failures and explicit deployment boundaries |
| Automated evidence and delivery | 10 | 9 | 10 | 34 unit/contract/API tests, 12 offline browser scenarios, real-provider E2E, build, screenshots, credential scan and audit |
| **Total** | **100** | **78** | **96** | **PASS ≥ 95** |

## Real-provider evidence

- MinerU state/model: complete / MinerU-HTML
- MinerU task: `df163c65-1e22-4c3c-a116-7cb674316a42`
- DeepSeek model: `deepseek-v4-flash`
- DeepSeek response: `dea6cc0f-3ca6-49e2-aae5-954a63344bdc`
- DeepSeek tokens: 2,935
- Parsed Markdown: 1,214 characters
- Published assets: 4
- Published source quotations: 8
- Evidence precision: honest section-level locator with quote SHA-256 and document SHA-256
- Credential leakage scan: PASS
- Dependency audit: cached/offline audit reports 0 vulnerabilities; two live npm audit requests timed out against the official IPv6 endpoint and are not represented as an online pass

## Remaining production deployment responsibilities

The four unawarded points are deliberate. This repository provides a production-shaped reference implementation, but a real enterprise deployment must integrate its own OIDC/RBAC/ABAC identity provider, encrypted database and object storage, immutable audit/event infrastructure, malware scanning, and provider contract/data-retention controls. Page/block coordinates are shown only when MinerU supplies a verifiable anchor; live evidence does not invent page numbers.

These items are disclosed in the UI under System Status and do not block the 96-point repository acceptance score. They do block claiming that the repository alone is a fully operated production SaaS.

## Acceptance artifacts

- `artifacts/e2e-report.md` — 12 offline browser scenarios
- `artifacts/real-e2e/report.md` — real MinerU + DeepSeek result
- `artifacts/real-e2e/publication.json` — sanitized publication snapshot
- `artifacts/screenshots/14-real-published-wiki.png` — real publish-to-Wiki flow
- `artifacts/enterprise-95-review/` — final desktop/mobile visual review
