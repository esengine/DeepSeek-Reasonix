# Windows startup and update recovery

This change addresses a shell that survives a failed service startup and continues holding Electron's existing data-home singleton. It does not change build identity comparison, the singleton namespace, Studio, conversation storage, or published release bytes.

## Runtime behavior

The shell owns startup, usable, recoverable failure, quitting and final cleanup states. `QuitSequencer` asks the existing business close policy, stops the service, performs each cleanup independently, and requests final Electron exit. A five-second shell exit fallback is armed only after service termination succeeds. A service state observer throwing cannot prevent shutdown. Startup and restart completions are fenced once shutdown starts.

Second launches show the current window, including minimized/tray windows, or recreate the shell recovery page. Build/contract mismatch pages recommend a complete package and do not expose a retry loop. Startup readiness has a 30-second deadline. The existing business close policy still controls normal tray hiding.

On Windows, `\\.\pipe\reasonix-shell-v1-<pid>` returns one read-only JSON status, schema 1, at most 16 KiB. It remains available after the Go service exits. It includes product identity, shell PID/version/generation, hashed canonical profile identity, lifecycle, service PID/state, visibility and renderer health/version. It contains no configuration or session contents. Consumers verify the OS pipe server PID, process user, executable path and process lifetime. Unknown schemas are not evidence of absence.

The internal argument `--reasonix-lifecycle-request=quit` is consumed by the shell before business argument dispatch. It asks the existing owner to exit; it never executes a supplied path.

## Shared process coordination

`internal/desktopinstance` is used by the Windows stable launcher, the installer-contained activator, and the versioned update helper. Coordination locks are scoped by canonical installation directory and Windows user SID, and acquired before the existing activation lock. Windows mutex ownership is bound to the OS thread.

The coordinator identifies the executable's installation layout and same-user process handles. Ordinary launch is scoped to one canonical data home. Installation covers affected processes throughout the installation. Studio and unrelated installation paths are excluded. Another installation owning the same profile blocks handoff. Legacy shells are identified by their Chromium singleton message window; a missing shell pipe does not establish that they exited.

Recovery requests normal exit, waits up to 20 seconds, and requires explicit confirmation to terminate the displayed process identities. Cancellation is the default. Creation time and the held process handle protect against PID reuse; descendants require a live verified parent chain. Processes are re-inspected after the confirmation and after termination. New processes invalidate the previous consent. Forced exit verification has a 10-second limit. Silent execution never grants consent.

Activation reuses the existing `ActivateVersion` transaction and `current.json` schema. A process check runs before file replacement and immediately before pointer publication; a late conflict triggers rollback. New launchers share the coordination lock. Older launchers cannot be made to honor it, so checks and final target readiness remain mandatory.

Readiness requires the target shell executable/version, strict production handshake, a live service in that release, a visible window, a real renderer `Version` call and the existing later frontend heartbeat. Existing legacy windows can be displayed by ordinary double-click without claiming they passed this newer readiness protocol. Update acceptance separately verifies the current target. Activation success followed by startup failure is reported as installed but not started; no automatic downgrade is introduced and retention cleanup is postponed until successful verification.

Local `desktop-shell/logs/recovery.log` uses one approximately 1 MiB file plus one backup. Shell logs include startup/exit attempts, state changes, process IDs and cleanup outcomes. No automatic upload was added.

Silent installer codes: 1602 = user cancelled; 1618 = recovery/conflict/ownership/exit blocker; 1603 = startup failure when returned by the coordinator; 1 = other activation error. The update helper records installed-versus-started results in its log. Its optional interactive notification requires the internal `REASONIX_INTERACTIVE_RECOVERY=1` environment flag; background mode does not show a blocking dialog.

## Validation record (2026-09-11)

Official immutable baseline: `desktop-v1.38.5`, `Reasonix-windows-amd64.zip`, SHA-256 `9c9be1e44a0d8e8b7511eba43f8b74f34ba80221b6d9b832a7cead30e4a4f132`.

On Windows 11 build 26200.9445 ARM64 (Parallels), the unmodified x64 portable baseline was launched through `Reasonix.exe` with an isolated data home. Its production handshake reported `build_mismatch` (`1.38.5` versus `v1.38.5`). Normal window close removed the failure window while leaving the shell alive. Another launch reached that same owner, which logged `secondInstance` with an exited service. This reproduces the reported failure shape without fault injection. It is x64 emulation on ARM64, not x64-native acceptance. The precise internal exception/reentrancy path in the old binary is not established by these observations.

The new native coordinator identified the legacy shell's isolated profile through its message window. Windows ARM64 tests exercised actual process handles, rejection of a changed creation time before termination, and mutex serialization/release. Electron deterministic tests cover dead-service exit, cleanup failure, shutdown/restart races and state observer exceptions. Activation tests cover a process appearing before pointer commit.

Local candidate `v1.38.7-recovery.1` (unsigned, production identity, unmodified assembled portable package) started through its stable launcher on Windows ARM64; the launcher returned 0 after shell/service/frontend readiness verification. This is development acceptance evidence, not acceptance of a signed release artifact.

| Target / gate | Status |
| --- | --- |
| Windows ARM64 native process tests | Passed; detailed latest results recorded during implementation |
| Windows ARM64 initial portable candidate startup | Passed |
| Windows ARM64 final installer/recovery/exit matrix | Pending |
| Windows x64 native package matrix | Unverified: no x64-native runner in this local session |
| macOS package exit/tray/failure recovery | Pending |
| Linux native package exit/tray/failure recovery | Unverified: no Linux native runner in this local session |
| Authenticode and final signed package acceptance | Pending existing release pipeline; no release performed |

Private user videos/logs are not committed. Tests use synthetic cases and isolated locally created profiles. Forced test cleanup is never counted as normal-exit success. Do not label the complete incident fixed until the outstanding package matrix is accepted.

## Release note draft

Windows startup and installation can detect and recover a leftover Reasonix process from an older version. Recovery preserves configuration and does not require uninstalling Studio. Ending an unresponsive old process requires confirmation because unsaved work may be lost. Ship in a subsequent patch release; do not overwrite 1.38.5 or 1.38.6.
