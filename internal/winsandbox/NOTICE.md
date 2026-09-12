This package is adapted from `github.com/SivanCola/windows-sandbox` at commit
`6b29dd09f9cb5a85d7ac646dd8ade74d207bb47b`.

The WRITE_RESTRICTED token, deterministic workspace/temp capability SID, exact
ACL grant, and restricted-token default-DACL design are adapted from DeepSeek
Harness at commit `c291e7961a`, package
`packages/sandbox/sandbox-windows-acl`:

https://github.com/deepseek-ai/DeepSeek-Harness

Both upstream projects are licensed under the MIT License. The license text is
kept in `LICENSE` in this directory. DeepSeek Harness carries:

Copyright (c) 2026 DeepSeek

Local modifications since vendoring:

- `lockWindowsRoots`/`acquireNamedMutex` take an optional notice writer and
  wait in slices, emitting a one-line message when a run queues behind another
  sandboxed command's per-root lock instead of blocking silently.
- Capability hashes are domain-separated for Reasonix and support multiple
  independently authorized writable roots.
- Existing Reasonix AppContainer read restrictions, forbid-read handling,
  Job Object UI restrictions, and crash-residue recovery are retained.
