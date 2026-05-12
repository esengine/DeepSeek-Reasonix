# Reasonix Desktop — code signing & notarization

Walkthrough for shipping a signed Reasonix Desktop bundle.

The release workflow at `.github/workflows/release.yml` reads everything
below from repository **Secrets** — nothing in this repo holds keys.
Without these secrets set the workflow still builds, but installers come
out unsigned (Windows shows SmartScreen warnings; macOS marks the app
"damaged" until the user right-clicks → Open).

## Tauri updater signing (all platforms)

Tauri's updater verifies bundle artifacts against a public key embedded
in the app. Generate once and commit the **public** half to
`tauri.conf.json`.

```bash
cd desktop
npx @tauri-apps/cli signer generate -w ~/.tauri/reasonix.key
```

Outputs:
- `~/.tauri/reasonix.key` — the **private** key. Never commit. Add a
  passphrase when prompted.
- `~/.tauri/reasonix.key.pub` — paste into `tauri.conf.json` under
  `plugins.updater.pubkey`, replacing `REPLACE_ME_RUN_tauri_signer_generate`.

Set repo secrets:
- `TAURI_SIGNING_PRIVATE_KEY` — full contents of `~/.tauri/reasonix.key`
- `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` — the passphrase

The workflow exports both as env vars; `tauri-action` picks them up and
signs the per-platform update bundle (`*.tar.gz.sig`, `*.zip.sig`,
`*.msi.zip.sig`, …) automatically.

## Windows — Authenticode

You need a **code signing certificate** from a CA Microsoft trusts
(DigiCert, Sectigo, SSL.com, …). EV certs avoid SmartScreen reputation
ramp; OV certs work but warn until enough installs build trust.

### One-time: export the cert as PFX

The CA either ships a `.pfx` directly or a `.cer` + private key. If you
get the latter, combine with `openssl`:

```bash
openssl pkcs12 -export \
  -inkey reasonix.key \
  -in reasonix.cer \
  -out reasonix.pfx \
  -name "Reasonix Code Signing"
```

Set a strong export password — needed below.

### Wire into the release workflow

Tauri v2 reads three env vars on Windows:

| Secret | What it is |
|---|---|
| `WINDOWS_CERTIFICATE` | base64-encoded contents of `reasonix.pfx` |
| `WINDOWS_CERTIFICATE_PASSWORD` | the PFX export password |

Encode the cert before adding the secret:

```bash
base64 -w0 reasonix.pfx > reasonix.pfx.b64
```

Then add a step to the matrix' Windows job that imports the cert and
points Tauri at it. The simplest approach uses the [Azure trusted
signing action] or the older `@tauri-apps/action`'s built-in
Authenticode path — set the secrets and `tauri-action` will pick them
up via `tauri-plugin-windows-installer`.

For the workflow already in this repo, add to the env block of the
`Build Tauri bundle` step:

```yaml
WINDOWS_CERTIFICATE: ${{ secrets.WINDOWS_CERTIFICATE }}
WINDOWS_CERTIFICATE_PASSWORD: ${{ secrets.WINDOWS_CERTIFICATE_PASSWORD }}
```

`tauri-action` v0 detects these and signs both the `.msi` and the
`.exe` produced by NSIS.

### Verify locally before pushing the tag

```powershell
signtool verify /pa /v Reasonix_0.40.0_x64-setup.exe
```

Output should include `Successfully verified` and the certificate's
common name.

[Azure trusted signing action]: https://github.com/Azure/trusted-signing-action

## macOS — Developer ID + notarization

Two separate signatures are required for a non-warning install:

1. **Code signing** with a Developer ID Application certificate — proves
   the bundle came from a paid Apple Developer account.
2. **Notarization** — Apple's automated malware scan, returns a ticket
   that gets stapled into the `.dmg` / `.app`.

### One-time: certificate

Enroll in the [Apple Developer Program] ($99/yr). In Xcode → Settings →
Accounts → Manage Certificates, create a `Developer ID Application`
cert. Export from Keychain as a `.p12` with a passphrase.

```bash
base64 -i ReasonixDeveloperID.p12 -o cert.p12.b64
```

### One-time: app-specific password for notarytool

Notarization uses the Apple ID, not the cert. At
<https://appleid.apple.com> → Sign-In and Security → App-Specific
Passwords, generate one labelled "reasonix notarytool". Save it — Apple
only shows it once.

### Repository secrets

| Secret | What it is |
|---|---|
| `APPLE_CERTIFICATE` | base64 of the `.p12` |
| `APPLE_CERTIFICATE_PASSWORD` | the `.p12` passphrase |
| `APPLE_SIGNING_IDENTITY` | the Common Name, e.g. `Developer ID Application: Your Name (TEAMID)` |
| `APPLE_ID` | your Apple ID email |
| `APPLE_PASSWORD` | the app-specific password from the previous step |
| `APPLE_TEAM_ID` | 10-character team identifier, visible in Apple Developer → Membership |

The release workflow already passes all six to `tauri-action`. Once
they're set, builds for both `macos-13` (Intel) and `macos-14` (Apple
Silicon) produce a signed + notarized `.dmg`.

### Verify locally

After downloading the artifact:

```bash
spctl -a -t open --context context:primary-signature -vvv Reasonix_0.40.0_aarch64.dmg
# expected: source=Notarized Developer ID
```

```bash
codesign --verify --deep --strict --verbose=2 /Applications/Reasonix.app
# expected: valid on disk + satisfies its Designated Requirement
```

[Apple Developer Program]: https://developer.apple.com/programs/

## Linux

No signing required — `.deb` and `.AppImage` ship plain. If the project
ever publishes a Flatpak or Snap, the respective store handles signing
on upload.

## Updater pubkey rotation

Rotating the updater key invalidates every previously installed
client's ability to verify updates. Avoid unless the private key
leaked. If it must happen:

1. Generate a new key pair (`tauri signer generate`).
2. Ship one transitional release signed with the **old** key whose
   notes tell users to download fresh installers manually.
3. Replace `tauri.conf.json#plugins.updater.pubkey` in the next release.
4. Update `TAURI_SIGNING_PRIVATE_KEY` in repo secrets.

## Troubleshooting

- **"errSecInternalComponent" on macOS runners** — Apple's `notarytool`
  needs the keychain unlocked. `tauri-action` handles this when the six
  Apple secrets are present; if you've inlined custom steps, add a
  keychain-unlock step before signing.
- **"The signature of the application is invalid"** on Windows after
  an update — almost always means the updater's `pubkey` in
  `tauri.conf.json` doesn't match the private key used by the workflow.
  Confirm both halves come from the same `signer generate` run.
- **`xcrun: error: unable to find utility "altool"`** — runner is on
  Xcode older than 13. Pin `xcode-select` or upgrade the runner image;
  `notarytool` (the replacement) is what Tauri v2 uses.
