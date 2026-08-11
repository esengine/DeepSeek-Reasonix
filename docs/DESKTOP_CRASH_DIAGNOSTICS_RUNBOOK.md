# Windows and Linux Desktop crash diagnostics runbook

<a href="./DESKTOP_CRASH_DIAGNOSTICS_RUNBOOK.zh-CN.md">简体中文</a>

This is the release, privacy, performance, and root-cause checklist for the
cross-platform Desktop diagnostics pipeline. Windows build `17763` remains a
priority experiment, not a code whitelist. A diagnostic release does not by
itself resolve a crash issue.

## Release order

1. Freeze one candidate SHA. Do not move or recreate a published tag.
2. Back up D1 and inspect `PRAGMA table_info` before applying
   `workers/crash-report/migrate-diagnostics-v2.sql`. If draft diagnostics-v2
   columns already exist, stop and create an additive reconciliation migration.
3. Verify `report_daily`, `report_installations`,
   `report_event_dimensions`, `diagnostics_meta`, their fingerprint/date
   indexes, and the ping window index. Confirm `installation_linked_since`.
4. Deploy the Worker first. Smoke-test old Report/Ping/Metrics payloads, a
   legacy `webview2` payload, and Windows/Linux `webRuntime` payloads using
   `channel=test`.
5. Build signed Windows and Linux artifacts from the frozen SHA. Complete the
   capability matrix and performance gates before a feature release.
6. Use the admin UI for the audited historical cleanup: ignore the synthetic
   `[go panic] safe` / `v9.9.9` group; resolve `72daba81` in
   `desktop-v1.19.3`; ignore the legacy `desktop.abnormal_exit` replay group.

## Privacy and compatibility smoke

Verify that old payloads may omit every new field and that legacy `webview2`
normalizes to `webRuntime`. Recovered and failed recovery events with the same
engine/kind/reason/exit code must share one fingerprint. Then verify:

- raw install IDs are absent from report samples, rendered HTML, application
  and audit logs, exports, and pending files;
- source modules are basenames; content, keys, accounts, hostnames, full paths,
  GPU models, and driver versions are absent;
- a repeated event increments daily/install/event-dimension counts without
  changing earlier event dimensions;
- deleting a test group deletes all three diagnostic aggregates;
- retention removes diagnostic facts, pings, and metric-user rows after 30
  days in bounded chunks;
- `channel=test` remains in the development namespace.

## Normal-experience gates

The candidate must keep pre-Wails work to one local configuration read, one
non-blocking ownership lock, and one small atomic lifecycle write. Runtime
discovery and all report/metric persistence run after Wails startup or on the
bounded background consumers; COM and GTK callbacks only enqueue or increment
an atomic drop counter. Diagnostic failure remains fail-open.

Compare the same SHA with diagnostics disabled. Diagnostic initialization p95
must be at most 10 ms and p99 at most 25 ms. DOM-ready p95 may regress by no
more than `max(20 ms, 2%)`; shutdown p95 by no more than 20 ms; idle CPU by less
than 0.1 percentage points; and idle RSS by at most 2 MiB. During 30 minutes of
normal use there must be no diagnostic reload, polling timer, user-visible
prompt, or network request beyond existing ping/metrics traffic.

## Capability matrix

Use the same candidate SHA throughout. Record Runtime and GPU/driver details in
the private lab worksheet; the client does not collect drivers.

| Platform | Required coverage |
| --- | --- |
| Windows 10 LTSC 2019 `17763` | VM plus a physical GPU system; system and latest Evergreen WebView2; GPU on/off |
| Windows 10 `19045` | x64 control; system and Evergreen WebView2 |
| Windows 11 stable | x64 physical smoke; current stable WebView2 |
| Windows arm64 | release cross-build plus one device smoke |
| Ubuntu 22.04 | WebKitGTK 4.0 and X11 |
| Ubuntu 24.04 | WebKitGTK 4.1, X11, and Wayland |
| Debian 12, Fedora stable, Arch rolling | capability smoke, local session, representative Intel/AMD/NVIDIA coverage |
| Remote sessions | Windows RDP and Linux remote/xrdp |

For every environment run 20 cold start/normal exits, 10 update restarts, a
60-minute workload, 50 minimize/restores, sleep/resume, display/DPI changes,
and remote connect/disconnect where applicable. A test-only build may terminate
the renderer/web process to verify exactly one recovery. Collect WER and
Reliability Monitor on Windows, and journal/coredump metadata on Linux. Dumps
or cores require explicit consent, private transfer, and deletion after use.

## Root-cause gate and follow-up

An environment association requires either two similar lab nodes reproducing
while controls do not, or three distinct online installations sharing a
fingerprint with at least 30 active installations and an impact rate three
times the control. A GPU workaround requires at least `2/20` GPU-on failures per
node, `0/40` GPU-off failures across two nodes, and two clean hours per node.
Scope any workaround by demonstrated capability/Runtime evidence, not distro
name or Windows build alone.

Treat integrity failures as signing/injection/security-software investigations,
out-of-memory as memory/session resource investigations, and Runtime clustering
as evidence for a later minimum-version/update policy. A successful renderer
recovery is not an application crash. Lifecycle-only abnormal exits require a
matching WER, journal, dump, or core before closure.

Observe production for seven complete UTC days: identity coverage should be at
least 95%; below 90% do not show an exact impact rate. Check legacy replay,
fatal/recovered/degraded coherence, recovery failures, platform/Runtime/GPU
rates, D1 growth, retention, and query latency daily. If evidence is
insufficient, keep the issue open and extend observation to 30 days. Ship a
later patch only for a demonstrated root cause and require `0/40` lab
reproductions after the fix.
