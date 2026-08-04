# Recovery and diagnostics (v1.20+)

Reasonix no longer ships a product `reasonix-guard` recovery shell. Crash
records, pending-update state, and configuration problems do not change the
next launch into a global Safe Mode.

## Prefer these tools

```text
reasonix doctor
reasonix doctor repair
reasonix crash report   # when available in your build
```

- **doctor** inspects configuration, derived desktop state, and common install
  problems without loading the Wails shell.
- **doctor repair** applies safe, explicit repairs the user opts into.
- Crash reports remain opt-in and never force a degraded product mode.

## Install layout (v1.20+)

Windows and Linux use a versioned install root:

```text
InstallRoot/
  reasonix-launcher[.exe]
  Reasonix.exe                 # Windows portable / Start Menu alias
  reasonix[-cli.exe]
  current.json
  versions/<version>/
    reasonix-desktop[.exe]
    reasonix-cli[.exe]
    reasonix-update-helper[.exe]
```

The thin launcher only reads `current.json` and starts the active desktop. It
never selects a previous version or enters Safe Mode.

## Upgrading from 1.18–1.19.1

If an older client is stuck on a pending update or Safe Mode loop:

1. Download the latest signed installer / package from the official download page.
2. Run it once (Windows: double-click; no need to uninstall or delete JSON).
3. Compatibility payloads may still include a one-shot binary named
   `reasonix-guard` that only migrates the flat layout into `current.json` and
   then deletes itself. That binary is not the old Guard product.

Do not manually delete `pending-update.json`, locks, or AppData as the recovery
procedure.

## macOS

macOS keeps LaunchServices launching the Wails app bundle directly. Updates
replace the signed `.app` atomically; there is no Guard process.
