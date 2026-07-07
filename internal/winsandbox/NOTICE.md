This package is adapted from `github.com/SivanCola/windows-sandbox` at commit
`6b29dd09f9cb5a85d7ac646dd8ade74d207bb47b`.

The original code is licensed under the MIT License. The license text is kept
in `LICENSE` in this directory.

Local modifications since vendoring:

- `lockWindowsRoots`/`acquireNamedMutex` take an optional notice writer and
  wait in slices, emitting a one-line message when a run queues behind another
  sandboxed command's per-root lock instead of blocking silently.
