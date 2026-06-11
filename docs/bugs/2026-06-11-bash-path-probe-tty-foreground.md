# Bash PATH Probe Steals TTY Foreground

## Summary

Reasonix TUI can be suspended by Unix job control after a login-shell PATH probe runs. The reproduced case came from the bash tool probe in `internal/tool/builtin/bash.go`, and the same probe pattern also exists in stdio plugin executable resolution in `internal/plugin/transport_stdio.go`: both can start an interactive login shell (`zsh -l -i -c ...`) in the same controlling-terminal session as the TUI. That shell can briefly become the terminal foreground process group and exit without returning foreground ownership to Reasonix.

Once Reasonix is no longer the terminal foreground process group, the next TTY read triggers `SIGTTIN` (`stopped (tty input)`), making the TUI appear to exit.

This is a terminal/session ownership bug, not a failure of the tool command that happened to be running. In the captured repro, the foreground process group changed before Reasonix received `SIGTTIN`; after the short-lived probe exited, the TTY still pointed at the probe's process group rather than Reasonix's process group.

## Reproduction Evidence

Local repro on macOS after clearing `~/Library/Caches/reasonix/job-control.log` and rebuilding Reasonix:

```text
2026-06-11T18:39:35.629684+08:00 start pid=41366 pgrp=41366 tpgid=41366
2026-06-11T18:39:50.054943+08:00 foreground change pgrp=41366 tpgid=41980 fg=41980 41366 41980 S+ ttys002 /bin/zsh -l -i -c printf '\n__REASONIX_BASH_PATH__=%s\n' "$PATH" 41982 41980 41980 R+ ttys002 (path_helper)
2026-06-11T18:39:56.829931+08:00 signal stopped (tty input)
2026-06-11T18:39:56.830653+08:00 handle stop stopped (tty input): release terminal
```

Process state while stuck:

```text
41366 67268 41366 41980 R    ttys002  reasonix
41382 41366 41382 41980 SN   ttys002  .../codegraph.js serve --mcp
42255 41366 41366 41980 T    ttys002  reasonix
```

`tpgid=41980` points to the short-lived PATH probe process group. `ps -g 41980` returned no processes after the probe exited, so the TTY foreground process group was left pointing at a dead group. Reasonix then attempted to read from the TTY while its own process group was `41366`, causing `SIGTTIN`.

## Root Cause

The failing call path is:

```text
bashCommandEnv
  -> cachedBashShellPATH
  -> defaultBashShellPATH
  -> runShellPATHCommand
  -> /bin/zsh -l -i -c 'printf ... "$PATH"'
```

There is an equivalent stdio plugin resolution path:

```text
resolveStdioExecutable
  -> stdioShellPATH
  -> defaultStdioShellPATH
  -> runShellPATHCommand
  -> /bin/zsh -l -i -c 'printf ... "$PATH"'
```

The `-i` login-shell probe is useful for matching the user's login PATH, but it must not run in the same controlling-terminal session as the Bubble Tea TUI.

### Why `SIGTTIN` Happens

On Unix, a terminal has one foreground process group. If a process outside that foreground process group reads from the terminal, the kernel sends `SIGTTIN` to stop it. The trace shows:

- Reasonix process group: `pgrp=41366`
- Terminal foreground process group after the probe: `tpgid=41980`
- Foreground owner at the transition: `/bin/zsh -l -i -c ...` plus macOS `path_helper`
- Later terminal read by Reasonix while not foreground: `signal stopped (tty input)`

The `ps -g 41980` check returned no processes after the probe exited, which means the TTY foreground process group was left pointing at a dead group. That is why Reasonix could not safely resume normal TTY input.

## Fix

Detach only the PATH probe command through a shared helper:

- Unix: run the probe in a new session via `SysProcAttr.Setsid = true`.
- Windows: keep the existing hidden-window behavior for the probe helper.

This covers both the bash tool probe and the stdio plugin probe while leaving normal bash tool process-group handling unchanged, so cancellation and process-tree cleanup continue to use the existing `Setpgid` / negative-pid kill behavior.

### Why This Scope Is Small

The change is limited to the short-lived login-shell PATH probes:

```text
internal/tool/builtin/bash.go       -> proc.PrepareShellPATHProbe(cmd)
internal/plugin/transport_stdio.go -> proc.PrepareShellPATHProbe(cmd)
```

It does not change:

- normal bash command execution;
- bash tool process-group cleanup (`Setpgid` and negative-pid kill);
- stdio plugin server process lifetime, pipes, or process tracking;
- the PATH merge logic or parsing logic;
- Bubble Tea terminal restore/release logic.

The helper exists in `internal/proc` so both PATH probes share the same terminal-session behavior and cannot drift again.

### Why `Setsid` Is Appropriate Here

The PATH probe is non-interactive from Reasonix's perspective:

- stdin is an empty string reader;
- stdout/stderr are captured by `CombinedOutput`;
- it has a 2 second timeout;
- its only purpose is to print the effective login-shell `PATH`.

Running this helper in a new session prevents the helper shell's job-control setup from changing the TUI's controlling terminal foreground process group. Since the helper communicates only through pipes and exits immediately, it does not need the TUI's controlling terminal.

This is intentionally not applied to normal bash tool commands in this PR, because those commands already rely on their existing process-group behavior for cancellation and cleanup.

## Review Checklist

- Bash PATH probe uses `proc.PrepareShellPATHProbe`.
- Stdio plugin PATH probe uses `proc.PrepareShellPATHProbe`.
- Unix implementation sets `SysProcAttr.Setsid = true`.
- Windows implementation preserves the existing hidden-window behavior.
- Existing `Setpgid` cleanup for regular bash commands is unchanged.
- Regression tests cover both bash and stdio plugin probe preparation on Unix.

## Verification

```sh
GOCACHE=/private/tmp/reasonix-gocache go test ./internal/tool/builtin -run 'TestShellPATHProbeDetachesControllingTerminal|TestReapTreeKillsGroupStragglers|TestBashMergesLoginShellPath' -count=1
GOCACHE=/private/tmp/reasonix-gocache go test ./internal/plugin -run 'TestStdioShellPATHProbeDetachesControllingTerminal|TestStdioFallsBackToShellPATHForCommandLookup|TestStdioUsesConfiguredPATHForCommandLookup' -count=1
GOCACHE=/private/tmp/reasonix-gocache make build
```
