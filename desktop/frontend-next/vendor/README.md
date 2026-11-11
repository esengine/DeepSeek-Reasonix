---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-11-10
---

# Vendored packages

`pnpm.overrides` in `package.json` points `redos-detector` at the tarball here, so that `pnpm install` can finish on Windows.

## Why

`@a2ui/web_core` depends on `redos-detector`, and its `isSafeRegex` uses it at run time to refuse regular expressions that can backtrack catastrophically. The check must stay.

Every published `redos-detector` 6.1.x tarball contains `dist/__mocks__/package-json:version.d.ts`. A colon is not a legal character in a Windows file name, so a package manager cannot write that path and the install stops with `os error 123` (issue #15067). Nothing reads the file: it is a type stub for a build-time module.

## What is changed

- `redos-detector-6.1.4.tgz` is the upstream 6.1.4 tarball with that one entry removed. The other 44 entries are byte-identical.
- The upstream tarball has the integrity `sha512-lPlka1rEH6kK42gtgokvvxMmpAvyc28DcRjQbGxeP5RJsJWgAduBOdGedVCDPdSyCr4ay/JDxq3nGYsJZyt8tA==`, the one the lockfile held before the override.
- The package is MIT licensed and carries its own `LICENSE`.

## Checking it

- `src/vendor-windows-names.test.ts` fails if the tarball holds a name Windows cannot create, so re-vendoring the unmodified upstream file is caught.
- To redo it: `npm pack redos-detector@6.1.4`, check the integrity above, drop the colon-named entry, write the archive back, then run `pnpm install --lockfile-only`.

## When to remove it

Delete the tarball, the `pnpm.overrides` entry and the test once `@a2ui/web_core` depends on a `redos-detector` that no longer ships a colon-named file, then run `pnpm install --lockfile-only`.
