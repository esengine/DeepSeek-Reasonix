package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
)

// answers is one machine's replies, keyed by what the probe looks for.
func answers(t *testing.T, root string, npm string, kernel bool) *fakeConn {
	t.Helper()
	return newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname -sm"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "npm --version"):
			if npm == "" {
				return remote.ExecResult{ExitCode: 127}, nil
			}
			return ok(npm + "\n")
		case strings.Contains(cmd, "command -v reasonix"):
			if kernel {
				return ok("bin /usr/local/bin/reasonix\nver 2.9.0\n" + allFlagsYes())
			}
			return ok("\n")
		default:
			return ok("")
		}
	})
}

func probeOf(t *testing.T, conn *fakeConn, opts Options) Report {
	t.Helper()
	rep, err := Probe(context.Background(), conn, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// The point of probing: one session says every missing piece, where a cold
// connect would have reported one per attempt.
func TestProbeReportsEveryClosedRouteAtOnce(t *testing.T) {
	root := testenv.TempDir(t)
	rep := probeOf(t, answers(t, root, "", false), Options{})
	if rep.Ready() {
		t.Fatalf("a machine with no npm, no binary and nothing to upload read as ready: %+v", rep)
	}
	if len(rep.Routes) != len(autoRouteOrder) {
		t.Fatalf("routes = %d, want one per auto route %v", len(rep.Routes), autoRouteOrder)
	}
	for _, want := range []struct {
		route string
		err   error
	}{
		{routeRemoteFetch, ErrRemoteFetchUnavailable},
		{InstallUpload, ErrPlatformMismatch},
		{InstallNPM, ErrNPMUnavailable},
	} {
		got, ok := routeNamed(rep, want.route)
		if !ok {
			t.Errorf("%s route was not reported", want.route)
			continue
		}
		if !errors.Is(got.Err, want.err) {
			t.Errorf("%s route: %v", want.route, got.Err)
		}
	}
	// The download route closes on an unwired fetcher, which carries no
	// sentinel on purpose; it still has to be reported as closed.
	if got, ok := routeNamed(rep, routeDownload); !ok || got.Err == nil {
		t.Errorf("download route = %+v, %v; want it reported and closed", got, ok)
	}
}

// routeNamed finds a route by name: the probe reports them in the order the
// installer would try, and an index pins that order into every assertion.
func routeNamed(rep Report, name string) (Route, bool) {
	for _, r := range rep.Routes {
		if r.Name == name {
			return r, true
		}
	}
	return Route{}, false
}

func TestProbeReadsTheMachine(t *testing.T) {
	root := testenv.TempDir(t)
	rep := probeOf(t, answers(t, root, "10.8.1", false), Options{})
	if rep.OS != "linux" || rep.Arch != "amd64" {
		t.Errorf("platform = %s/%s", rep.OS, rep.Arch)
	}
	if rep.NPM != "10.8.1" {
		t.Errorf("npm = %q", rep.NPM)
	}
	if !rep.Ready() {
		t.Error("a machine with npm has a route and did not say so")
	}
}

// A kernel already there needs no route at all.
func TestProbeFindsAnInstalledKernel(t *testing.T) {
	root := testenv.TempDir(t)
	rep := probeOf(t, answers(t, root, "", true), Options{MinVersion: "2.0.0"})
	if rep.Kernel == "" || rep.Version != "2.9.0" {
		t.Fatalf("kernel = %q %q", rep.Kernel, rep.Version)
	}
	if !rep.Ready() {
		t.Error("a machine that already has one read as not ready")
	}
}

// The same binary on the same platform is uploadable; a different one is the
// mismatch a connect would have failed on halfway through.
func TestProbeUploadRouteFollowsThePlatform(t *testing.T) {
	root := testenv.TempDir(t)
	same := probeOf(t, answers(t, root, "", false), Options{LocalBinary: "/x/reasonix", LocalGOOS: "linux", LocalGOARCH: "amd64"})
	if !same.Routes[1].OK() {
		t.Errorf("same platform should upload: %v", same.Routes[1].Err)
	}
	other := probeOf(t, answers(t, root, "", false), Options{LocalBinary: "/x/reasonix", LocalGOOS: "darwin", LocalGOARCH: "arm64"})
	if !errors.Is(other.Routes[1].Err, ErrPlatformMismatch) {
		t.Errorf("cross platform should not upload: %v", other.Routes[1].Err)
	}
}

// serve_install = never is one closed route, not three — the reader is not
// owed a report on npm when nothing may be installed anyway.
func TestProbeHonoursInstallNever(t *testing.T) {
	root := testenv.TempDir(t)
	rep := probeOf(t, answers(t, root, "10.8.1", false), Options{Install: InstallNever})
	if len(rep.Routes) != 1 || !errors.Is(rep.Routes[0].Err, ErrInstallDisabled) {
		t.Fatalf("routes = %+v", rep.Routes)
	}
	if rep.Ready() {
		t.Error("install-never with no kernel is not ready")
	}
}
