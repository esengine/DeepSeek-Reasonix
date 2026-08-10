# intelifar IP Wiki Platform Design

## Product boundary

This delivery turns the web surface of DeepSeek-Reasonix into an enterprise long-document IP analysis and Wiki workspace. The Reasonix Go kernel remains intact as the future agent/runtime foundation; the Astro site becomes a self-contained, offline-demonstrable product surface with deterministic fixtures for acceptance testing.

The implemented vertical slice covers the six acceptance paths from the attached technical report: multi-format intake and classification, schema-driven IP extraction, Wiki generation, irreversible redaction preview, exact provenance, and lifecycle governance with audit evidence.

## Considered approaches

1. Rebuild the Wails desktop frontend. This offers the deepest host integration but has a very large regression surface and would make a browser-verifiable delivery unnecessarily slow.
2. Add a separate prototype beside the repository. This is fast but does not meaningfully transform the referenced project.
3. Transform the existing Astro web surface while preserving the Go kernel. This is the selected approach because it is part of the upstream product, builds independently, and supports complete browser E2E verification.

## Experience architecture

The application is organized around operator jobs rather than AI chat:

- Command center: operational KPIs, processing pipeline, risk queue, and recent assets.
- Document center: intake, format coverage, classification, and analysis launch.
- Analysis studio: visible processing stages, schema extraction, confidence, and provenance.
- IP asset library: searchable structured assets with ownership and sensitivity state.
- IP Wiki: executive narrative, knowledge relations, citations, and source navigation.
- Redaction and provenance: redacted preview, permission-aware reveal, and source highlight.
- Governance: share/transfer controls, permission matrix, audit trail, and export.
- System health: local processing posture and service readiness.

The primary end-to-end flow is document intake -> classification -> extraction -> Wiki -> redaction -> provenance -> lifecycle action -> audit evidence.

## Visual direction

The interface follows intelifar's published brand baseline: primary `#635BFF`, white and soft gray surfaces, ink text, rounded controls, and restrained violet shadows. The product expression is "precision console meets knowledge archive": a dark navigation rail, paper-like work surfaces, small monospaced evidence labels, fine grid texture, and editorial hierarchy. The official light and dark intelifar logo assets are used directly.

The layout supports desktop and compact tablet/mobile widths. Focus states, semantic buttons, live status text, reduced-motion behavior, and non-color-only state labels are required.

## State and error handling

Demo state is deterministic and stored locally. Intake validates document name and classification before creating a task. Analysis progression has explicit pending/running/complete states. Share actions require a recipient and expiry. Destructive-looking lifecycle controls are simulated and always create visible audit events rather than mutating external systems.

Empty search results, invalid forms, restricted provenance, and processing failures receive inline feedback plus a live toast. No credentials or external APIs are required for the acceptance flow.

## Verification

Pure state functions receive Node contract tests. Astro production build verifies integration. Browser E2E exercises every core navigation surface, the full intake-to-analysis flow, asset detail, provenance, redaction reveal, share governance, theme persistence, mobile layout, and audit export. Final desktop and mobile screenshots are stored under `artifacts/screenshots/`, with a machine-readable test report and delivery tree alongside them.
