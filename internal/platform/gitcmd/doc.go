// Package gitcmd builds every git invocation Reasonix runs on its own behalf:
// the status readout, workspace change listings and diffs, worktree
// management, and plugin source checkouts.
//
// Host git never runs a program named by repository configuration. A
// repository's .git/config, its includes and its hooks are data written by
// whoever produced the repository, or by any command able to write inside it,
// and git executes several of them during ordinary read-only work:
//
//   - Fixed keys are overridden with -c, which outranks repository config:
//     core.fsmonitor (empty: old git reads "false" as a hook name),
//     core.hooksPath, signature verification, diff.submodule, auto-maintenance
//     and safe.bareRepository=explicit. submodule.recurse is off for tree
//     updates, and elsewhere where the repository sets it. GIT_NO_LAZY_FETCH
//     keeps a missing object from becoming a fetch (ObjectsMissing names it).
//   - diff, log and show carry --no-ext-diff and --no-textconv; status and diff
//     carry --ignore-submodules=dirty, since inspecting a submodule's working
//     tree starts git inside it under the submodule's own config.
//   - Filter and merge drivers defined at local or worktree scope are listed by
//     git itself before each invocation (includes followed) and overridden
//     empty; user and system drivers stay live. A name -c cannot address stops
//     the invocation (ErrRepositoryDrivers).
//
// Which repository is read is settled once. A session resolves its workspace
// with Open before any command runs, and every later call through the Repo
// names its git dir, common dir and work tree explicitly, so a nested .git, a
// commondir file or a damaged HEAD written afterwards cannot substitute
// another repository's config. StageAll stages without entering a nested
// repository, and host-created worktrees are opened as they are added.
//
// A checkout during `worktree add` runs in a child past the driver listing, so
// callers add --no-checkout and populate through gitcmd in the new worktree.
// Remote transport settings reach only network commands, which run outside
// any repository (see Detached).
package gitcmd
