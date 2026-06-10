# Third-Party Notices

This file is a distribution checklist for the GEEMu compact full GitHub package.
It is not legal advice. Before public release, verify the current upstream
licenses and terms for every bundled third-party source.

Checked date: 2026-06-04

## Current Package Profile

This `GITHUB/` folder includes:

- GEEMu's own `SKILL.md`, `references/`, `templates/`, `examples/`, and scripts.
- `awesome-gee-community-datasets/community_datasets.csv`.
- A sanitized derived local knowledge database under `gee_vector_db/`.

This folder intentionally excludes:

- Raw `google-earth-engine/` documentation snapshots.
- Raw `geemap/` documentation snapshots.
- Development tests, caches, temporary run outputs, and private local paths.

## Google Earth Engine Documentation

- Upstream: https://developers.google.com/earth-engine
- Site policies: https://developers.google.com/terms/site-policies
- Earth Engine terms: https://earthengine.google.com/terms/

Google Developers site policies generally license documentation under CC BY 4.0
and code samples under Apache 2.0, except where otherwise noted. Some pages,
datasets, images, or community tutorials may carry additional notices.

The compact package does not bundle the raw documentation snapshot, but the
local knowledge DB may contain derived excerpts and source metadata.

## geemap

- Upstream: https://github.com/gee-community/geemap
- Website: https://geemap.org
- License noted upstream: MIT

The compact package does not bundle the raw geemap documentation snapshot, but
the local knowledge DB may contain derived excerpts and usage metadata.

## Awesome GEE Community Datasets

- Upstream: https://github.com/samapriya/awesome-gee-community-datasets
- Catalog website: https://gee-community-catalog.org
- Catalog license page: https://gee-community-catalog.org/license/

The community catalog project is published under CC BY 4.0. Individual dataset
rows may list different dataset licenses in `community_datasets.csv`. Before
using or recommending a community dataset, check its `license`, `license_text`,
`docs_page`, and provider terms.

## Local Experience Notes

The local knowledge database may include selected GEE-related practical notes
and personal workflow knowledge. Public redistribution should only include text
you own or have permission to redistribute. Review screenshots, figures,
quotations, and third-party excerpts separately.

## Local Knowledge Database Files

- Local folder: `gee_vector_db/`
- Files: `chunks.jsonl`, `documents.jsonl`, `manifest.json`

The local knowledge database is a derived artifact. `chunks.jsonl` and
`documents.jsonl` may contain excerpts or metadata from third-party sources. The
files in this package have been sanitized to remove private local machine paths
and public-facing source descriptions that are not needed by the skill.

## Release Checklist

Before publishing:

1. Choose a license for GEEMu's original files.
2. Keep this notice file in the repository.
3. Preserve source URLs and attribution where available.
4. Confirm whether the derived local knowledge database is appropriate for public release.
5. Preserve per-row dataset licenses and docs links in the community CSV.
6. Remove private project IDs, asset IDs, credentials, proxy credentials, tokens,
   cookies, and local machine paths.
