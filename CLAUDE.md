# Project Notes

- Active development for Reasonix 1.0 happens on `main-v2`; the legacy TypeScript line is not the target for new desktop/Go features.
- Skill discovery is centralized in `internal/skill` and assembled in `internal/boot`; keep prompt index, slash invocation, `run_skill`, builtin subagent wrappers, and desktop capabilities in sync.
- `[skills].disabled_skills` is a persistent config preference. Disabled skills stay visible in management surfaces but must be absent from executable skill surfaces.
- Local Windows Codex environments may not have `go`/`gofmt`; when unavailable, use `git diff --check` locally and rely on GitHub Actions for `gofmt`, `go vet`, `go build`, and `go test`.
