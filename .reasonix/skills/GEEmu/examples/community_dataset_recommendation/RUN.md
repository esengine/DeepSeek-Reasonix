# GEEMu Run Log

## User Goal

Find candidate community datasets related to precipitation, rainfall, and climate.

## Questions and Answers

- Cloud Project: not needed for local CSV search
- Auth mode: not needed
- Network/proxy: not needed for local CSV search
- Proxy host and port: not used
- Python or JavaScript: Python
- Dataset source: `awesome-gee-community-datasets/community_datasets.csv`
- Output target: `community_candidates.csv`

## Parameter Ledger

| Item | Default/Old | Chosen/New | Reason | Effect/Risk |
|---|---|---|---|---|
| search fields | none | title, tags, thematic_group, provider, type | structured recommendation | misses datasets with unrelated wording |
| result limit | none | 15 | keep review focused | may omit weaker candidates |
| verification | CSV row only | docs page required before final code | avoid wrong band/unit assumptions | needs follow-up |

## Assumptions and Risks

- CSV search finds candidates, not final verified datasets.
- Band schemas and units must be checked from `docs_page`.
- Per-dataset license must be reviewed before use.
