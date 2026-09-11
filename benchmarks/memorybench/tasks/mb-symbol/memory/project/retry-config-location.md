---
name: retry-config-location
description: Retry limits live in policy/retry.toml, not env vars
metadata:
  type: project
  scope: project
---

Retry limits are configured in `policy/retry.toml` (the MEMKEY-SYM9K policy file), key `max_backoff_ms`. Never add them to .env.
