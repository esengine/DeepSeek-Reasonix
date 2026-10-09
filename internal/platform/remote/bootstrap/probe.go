package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/platform/releaseasset"
)

// Routes the auto strategy takes that are not serve_install values — nobody
// can ask for either alone. routeRemoteFetch has the machine download its own
// release; routeDownload fetches it here and pushes it over SFTP.
const (
	routeRemoteFetch = "remote_fetch"
	routeDownload    = "download"
)

// Report is what one machine can and cannot do, read in a single session. A
// cold connect walks these one at a time and stops at the first that fails, so
// every failed attempt teaches the reader exactly one missing piece; this
// answers all of them at once.
type Report struct {
	OS, Arch, Home string
	// Kernel is a reasonix already there that this caller could drive.
	Kernel, Version string
	// Outdated is one that is there and below the floor — having an old one is
	// a different next move from having none.
	Outdated string
	// NPM is npm's own version over there; empty means it would not run.
	NPM string
	// Downloader is what that machine would fetch its own release with — curl,
	// wget, or PowerShell's own. Empty means it cannot, which closes the route
	// the auto strategy takes first.
	Downloader string
	Routes     []Route
	env        loginEnv
}

// Route is one way a kernel could reach that machine, and what stands in the
// way when something does. Err carries this package's own sentinels, so a
// probe and a failed connect name the same obstacle the same way.
type Route struct {
	Name string // npm | upload | download | never
	Err  error  // nil when the route is open
}

// OK reports whether this route could be taken.
func (r Route) OK() bool { return r.Err == nil }

// Ready reports whether a connect would find or install a kernel. It promises
// a route exists, never that it will land: npm can still refuse to write, and
// only trying finds that out.
func (r Report) Ready() bool {
	if r.Kernel != "" {
		return true
	}
	for _, route := range r.Routes {
		if route.OK() {
			return true
		}
	}
	return false
}

// Probe reads everything a first connect depends on, and changes nothing over
// there.
func Probe(ctx context.Context, conn Conn, opts Options) (Report, error) {
	fs, err := conn.SFTP()
	if err != nil {
		return Report{}, err
	}
	target, goos, goarch, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return Report{}, err
	}
	// Home as that machine spells it: the file layer's /C:/Users/... is not a
	// path anybody there would type.
	conn, env := connWithLoginEnv(ctx, conn, target)
	rep := Report{env: env, OS: goos, Arch: goarch, Home: target.NativePath(home)}
	// The whole set, not the subset this call carries: a probe answers for a
	// connect, and every connect that opens a pane publishes a broker. Calling
	// a kernel usable that the connect then refuses is what this prevents.
	flags := LaunchFlags(true)
	found := probeBinaries(ctx, conn, target, uploadedBinPath(home, target.Executable()), flags)
	for _, c := range found {
		if c.usable(opts.MinVersion, flags) {
			rep.Kernel, rep.Version = c.path, c.version
			break
		}
	}
	rep.Outdated = outdated(found, opts.MinVersion)
	if res, execErr := conn.Exec(ctx, target.NPMVersion()); execErr == nil && res.ExitCode == 0 {
		rep.NPM = strings.TrimSpace(string(res.Stdout))
	}
	if res, execErr := conn.Exec(ctx, target.Downloader()); execErr == nil && res.ExitCode == 0 {
		rep.Downloader = strings.TrimSpace(string(res.Stdout))
	}
	rep.Routes = routesFor(rep, opts, goos, goarch)
	return rep, nil
}

// routesFor mirrors what ensureBinary would try, in the order it would try
// them. The two have to agree: a probe reporting a route the installer never
// takes is worse than no probe at all.
func routesFor(rep Report, opts Options, goos, goarch string) []Route {
	strategy := opts.Install
	if strategy == "" {
		strategy = InstallAuto
	}
	if strategy == InstallNever {
		return []Route{{Name: InstallNever, Err: ErrInstallDisabled}}
	}
	names := []string{strategy}
	if strategy == InstallAuto {
		names = autoRouteOrder
	}
	out := make([]Route, 0, len(names))
	for _, name := range names {
		out = append(out, Route{Name: name, Err: routeClosed(name, rep, opts, goos, goarch)})
	}
	return out
}

// routeClosed names what stands in one route's way, or nil when nothing
// visibly does. It answers for exactly the routes runRoute takes, so the two
// cannot drift into a probe promising something the installer never tries.
func routeClosed(name string, rep Report, opts Options, goos, goarch string) error {
	switch name {
	case routeRemoteFetch:
		if opts.ResolveDownload == nil {
			return fmt.Errorf("%w: this build cannot name a release", ErrRemoteFetchUnavailable)
		}
		if err := releaseasset.SupportsTarget(opts.ProductVersion, goos, goarch); err != nil {
			return fmt.Errorf("%w: %w", ErrNoReleaseForBuild, err)
		}
		if rep.Downloader == "" {
			return fmt.Errorf("%w: neither curl nor wget is installed", ErrRemoteFetchUnavailable)
		}
		return nil
	case InstallNPM:
		if rep.NPM == "" {
			return npmUnavailable(rep.env, nil)
		}
		return nil
	case InstallUpload:
		switch {
		case opts.LocalBinary == "":
			return fmt.Errorf("%w: no local Reasonix CLI to upload", ErrPlatformMismatch)
		case opts.LocalGOOS != goos || opts.LocalGOARCH != goarch:
			return fmt.Errorf("%w: local is %s/%s, remote is %s/%s",
				ErrPlatformMismatch, opts.LocalGOOS, opts.LocalGOARCH, goos, goarch)
		}
		return nil
	case routeDownload:
		if opts.FetchBinary == nil {
			// No sentinel: this is the caller not wiring a fetcher, not anything
			// about the machine, and it never happens in a shipped build.
			return errors.New("this build cannot fetch a release")
		}
		if err := releaseasset.SupportsTarget(opts.ProductVersion, goos, goarch); err != nil {
			return fmt.Errorf("%w: %w", ErrNoReleaseForBuild, err)
		}
		return nil
	}
	return fmt.Errorf("bootstrap: unknown install route %q", name)
}
