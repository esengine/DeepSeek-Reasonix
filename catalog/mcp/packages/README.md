# MCP catalog package inputs

Place each release-ready plugin tree in a directory named after its catalog
entry ID. Catalog CI recomputes the deterministic package tree and manifest
SHA-256 values from these directories before a catalog can be signed.

These inputs are catalog-release artifacts, not runtime plugin installs. The
private minisign key remains only in the `MCP_CATALOG_MINISIGN_PRIVATE_KEY`
release secret.
