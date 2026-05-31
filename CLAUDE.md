# Project Notes

- Upstream currently exposes `origin/v1` and `origin/main-v2`; there is no active upstream `main`, so do not use a stale local `origin/main` as the sync baseline.
- For PR 2209 and related TypeScript work, use `origin/v1` as the merge-base branch unless GitHub says otherwise.
- Common checks: `npm run lint`, `npm run typecheck`, targeted `npm test -- ...`, and `npm run verify` when the local environment can run the full suite.
- Do not add banner separator comments such as `// --- section ---`; `tests/comment-policy.test.ts` enforces the project comment policy.
- Skill loading changes must preserve symlink support in `SkillStore.readEntry()` and built-in skill description localization through `builtinSkillDescription()`.
