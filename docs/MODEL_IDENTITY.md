# Model IDs and capability overrides

Model IDs are case-sensitive identifiers. Reasonix preserves their spelling when saving configuration and sending requests. `Model-A` and `model-a` may coexist, but their image capability, context window, output limit, and reasoning overrides are independent. Model search may ignore case without changing the selected ID.

Use the exact ID accepted by the configured API endpoint. Display names and aliases are not implicit alternate spellings: if a service exposes an alias, configure that exact API identifier. Reasonix does not infer aliases by folding case. Adding a model validates its ID and numeric settings locally; a connection test determines whether the endpoint accepts it. An unlisted model can still be callable.

Unknown DeepSeek models allow manual image capability declarations. This does not prove that the endpoint supports images. Known text-only V4 Flash and V4 Pro remain blocked; video and PDF transport are not enabled by this setting.

## Existing configuration

The configuration format is unchanged. Existing exact-ID settings continue to work. Overrides or legacy vision-list entries whose capitalization differs from the configured model ID are retained but no longer applied to that model. Correct the keys to match the actual ID; do not automatically lowercase them. An older Reasonix version may still apply its previous case-insensitive fallback when reading the same file, so it does not provide the new isolation guarantee.
