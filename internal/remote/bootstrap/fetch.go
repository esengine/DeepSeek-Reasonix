package bootstrap

import (
	"strings"

	"reasonix/internal/releaseasset"
)

// FetchCommand builds the `sh -c` script that has the remote download its own
// kernel and verify it. The digest is resolved on this side and interpolated
// here: a host fetching both the archive and its checksums would be verifying
// one against another the same network handed it. Every operand is
// single-quote-escaped, the digest included.
func FetchCommand(d releaseasset.CLIDownload, dir, bin string) string {
	return strings.Join([]string{
		"set -e",
		"D=" + shellQuote(dir),
		`T="$D/.fetch"`,
		`rm -rf "$T"`,
		`mkdir -p "$T" "$D"`,
		// On the way out, however it goes. `set -e` is what made a failed
		// download leave the staging directory — with whatever it had already
		// written in it — for the next attempt to find and start beside.
		`trap 'cd "$D" 2>/dev/null; rm -rf "$T"' EXIT`,
		`cd "$T"`,
		"A=" + shellQuote(d.Asset),
		"U=" + shellQuote(d.URL),
		// Bounded on connecting and on stalling, never on total time: 40MB over
		// a slow link is not a failure, while a connection that opens and then
		// never speaks is what a filtered CDN looks like — and this runs first.
		`if command -v curl >/dev/null 2>&1; then` +
			` curl -fsSL --retry 2 --connect-timeout 15 --speed-time 30 --speed-limit 1024 -o "$A" "$U";` +
			` elif command -v wget >/dev/null 2>&1; then` +
			` wget -q --tries=2 --connect-timeout=15 --read-timeout=30 -O "$A" "$U";` +
			` else echo "bootstrap: neither curl nor wget is installed" >&2; exit 1; fi`,
		"S=" + shellQuote(d.SHA256),
		`if command -v sha256sum >/dev/null 2>&1; then echo "$S  $A" | sha256sum -c - >/dev/null;` +
			` elif command -v shasum >/dev/null 2>&1; then echo "$S  $A" | shasum -a 256 -c - >/dev/null;` +
			` else echo "bootstrap: neither sha256sum nor shasum is installed" >&2; exit 1; fi`,
		`tar xzf "$A"`,
		// By name, at any depth: the release has shipped the binary both at the
		// archive root and one directory down, and the extractor here matches
		// on the base name for the same reason.
		"F=$(find . -type f -name " + shellQuote(d.Executable) + " | head -n 1)",
		`[ -n "$F" ] || { echo "bootstrap: no ` + d.Executable + ` in the archive" >&2; exit 1; }`,
		"B=" + shellQuote(bin),
		`mv "$F" "$B"`,
		`chmod 755 "$B"`,
		`cd "$D"`,
	}, "; ")
}

// WindowsFetchCommand is FetchCommand for a Windows remote, which ships a zip
// and has neither curl's flags nor sha256sum. Same shape, same guarantee: the
// digest comes from this side and the archive is discarded unless it matches.
func WindowsFetchCommand(d releaseasset.CLIDownload, dir, bin string) string {
	script := strings.Join([]string{
		"$ErrorActionPreference='Stop'",
		"$d = " + psQuote(toShellPath(dir)),
		"$t = Join-Path $d '.fetch'",
		"if (Test-Path -LiteralPath $t) { Remove-Item -Recurse -Force -LiteralPath $t }",
		"New-Item -ItemType Directory -Force -Path $d,$t | Out-Null",
		"$a = Join-Path $t " + psQuote(d.Asset),
		"$ProgressPreference='SilentlyContinue'",
		// try/finally for the reason the POSIX side has a trap: Stop turns the
		// first failure into a throw, and the cleanup after it never ran.
		"try {",
		// Invoke-WebRequest has no connect timeout of its own, so a short HEAD
		// runs first: without it a stalled connection would hold the download's
		// whole budget before the next route is tried.
		"Invoke-WebRequest -UseBasicParsing -Method Head -Uri " + psQuote(d.URL) + " -TimeoutSec 20 | Out-Null",
		"Invoke-WebRequest -UseBasicParsing -Uri " + psQuote(d.URL) + " -OutFile $a -TimeoutSec 900",
		"$h = (Get-FileHash -Algorithm SHA256 -LiteralPath $a).Hash.ToLower()",
		"if ($h -ne " + psQuote(strings.ToLower(d.SHA256)) + ") { throw 'bootstrap: SHA-256 mismatch on the downloaded archive' }",
		"Expand-Archive -LiteralPath $a -DestinationPath $t -Force",
		"$f = Get-ChildItem -Path $t -Recurse -File -Filter " + psQuote(d.Executable) + " | Select-Object -First 1",
		"if (-not $f) { throw 'bootstrap: no " + d.Executable + " in the archive' }",
		"Move-Item -Force -LiteralPath $f.FullName -Destination " + psQuote(toShellPath(bin)),
		"} finally { Remove-Item -Recurse -Force -LiteralPath $t -ErrorAction SilentlyContinue }",
	}, "; ")
	return psCommand(script)
}
