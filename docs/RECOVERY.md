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

## Upgrading from 1.18–1.19.x

If an older client is stuck on a pending update or Safe Mode loop:

1. Download the latest signed installer / package from the official download page.
2. Install it directly over the current copy (Windows: double-click; macOS:
   replace `Reasonix.app`). Do not uninstall first: keeping the existing install
   root lets the compatibility migrator prove which stale transaction it owns.
3. Start Reasonix once and confirm **Settings > Updates** shows the installed
   version before trying another in-app update.
4. Compatibility payloads may still include a one-shot binary named
   `reasonix-guard` that only migrates the flat layout into `current.json` and
   then deletes itself. That binary is not the old Guard product.

Do not manually delete `pending-update.json`, locks, or AppData as the recovery
procedure.

## In-app update stuck

If Settings → Updates (or the top banner) reports that the previous update has
not finished (`pending update already exists`, `awaiting startup health`, or
`handoff backup` errors):

1. Click **Discard previous update** in the banner or Settings, then **Retry**.
2. If that button is missing or fails, quit Reasonix fully and start it once so
   startup can commit or retire the probationary transaction, then retry the
   in-app update.
3. If in-app update still fails, download the latest signed installer from the
   official download page and install it **over** the current copy without
   uninstalling first.
4. On macOS, also allow Reasonix under System Settings → Privacy & Security →
   App Management when the dialog appears; a leftover
   `Reasonix.app.reasonix-update-backup` that TCC will not let the app remove
   may still require the official installer path.

If the Windows installer reports `Reasonix layout activation failed`, expand
the installer details and copy the lines under `Reasonix layout activator
output:`. Current installers preserve the activator's concrete error instead of
showing only exit code 1.

## macOS

macOS keeps LaunchServices launching the Wails app bundle directly. Updates
replace the signed `.app` atomically; there is no Guard process.

After the replacement window becomes visible, Reasonix commits only the exact
pending transaction captured before launch. Legacy transactions that lack a
backup digest, or whose backup is already gone, are retired automatically only
after the running executable is proven to belong to that target bundle. Any
surviving unknown backup and the original transaction are archived for recovery;
they are not deleted or trusted as an automatic rollback source.
