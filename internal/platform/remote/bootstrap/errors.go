package bootstrap

import "errors"

// A cold connect installs a kernel on the far side, and the ways that goes
// wrong are not one failure. Which one it was decides the next move — install
// node, fix a PATH, switch strategies, or go do it by hand — and a sentence
// leaves the reader to work that out from prose in a language the window may
// not even be in. Callers match with errors.Is; the wrapped text is for logs.
var (
	// The machine has no usable reasonix and the strategy forbids installing.
	ErrInstallDisabled = errors.New("bootstrap: install disabled by serve_install")

	// The machine could not fetch its own release: no curl or wget, no route to
	// the release host, or a digest that did not match what it downloaded.
	ErrRemoteFetchUnavailable = errors.New("bootstrap: the remote could not fetch the release itself")

	// npm would not run, or ran and refused. Usually no Node.js over there.
	ErrNPMUnavailable = errors.New("bootstrap: npm unavailable on the remote")

	// npm reported success and left the binary somewhere the login shell does
	// not look — a prefix set for an interactive shell this session is not.
	ErrNPMOutsidePath = errors.New("bootstrap: npm installed outside the login PATH")

	// The local binary cannot run on that machine, and no release stood in.
	ErrPlatformMismatch = errors.New("bootstrap: local binary does not fit the remote platform")

	// The build asking for the install has no published release to install
	// from — a source build, or a line that ships no CLI archive. Nothing about
	// the remote is wrong, and reporting one of its routes as the cause sends
	// the reader to fix a machine that is fine.
	ErrNoReleaseForBuild = errors.New("bootstrap: this build has no published release to install from")

	// Every strategy was tried and none landed a kernel.
	ErrNoInstallPath = errors.New("bootstrap: no way to install a kernel on this machine")

	// One landed and will not run: a truncated upload, a wrong build, noexec.
	ErrBinaryNotRunnable = errors.New("bootstrap: the installed binary does not run there")

	// It runs; it never reported a port. The launch is what to look at.
	ErrServeDidNotStart = errors.New("bootstrap: serve never reported a port")
)
