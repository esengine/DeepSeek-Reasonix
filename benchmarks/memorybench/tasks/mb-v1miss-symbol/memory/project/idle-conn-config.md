---
name: idle-conn-config
description: Where ServerCfg.MaxIdleConnSecs lives
metadata:
  type: project
  scope: project
---

Idle connection limits are ServerCfg.MaxIdleConnSecs, persisted as max_idle_conn_secs in conf/server.toml (MEMKEY-VM1SYM). Never add them to env.
