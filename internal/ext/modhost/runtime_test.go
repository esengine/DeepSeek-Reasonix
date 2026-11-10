package modhost

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func fakeProbe(version string) func(context.Context, string, []string) (string, error) {
	return func(context.Context, string, []string) (string, error) { return version, nil }
}

func TestStudioRunsItsOwnBinaryAsNodeAndChecksTheVersion(t *testing.T) {
	r := Resolver{Electron: "/Applications/Reasonix.app/Contents/MacOS/Reasonix", Probe: fakeProbe("24.11.0")}
	rt, err := r.Resolve(context.Background())
	if err != nil || rt.Kind != "electron" || rt.Env["ELECTRON_RUN_AS_NODE"] != "1" || rt.Version != "24.11.0" {
		t.Fatalf("runtime = %+v, %v", rt, err)
	}
	r.Probe = fakeProbe("20.1.0")
	if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrRuntimeUnsupported) {
		t.Fatalf("an old runtime = %v, want ErrRuntimeUnsupported", err)
	}
}

func TestNodeOnPathIsUsedOnlyWhenAbsoluteOutsideTheProjectAndNewEnough(t *testing.T) {
	root := testenv.TempDir(t)
	good := filepath.Join(testenv.TempDir(t), "node")
	inside := filepath.Join(root, "bin", "node")
	for name, tc := range map[string]struct {
		path    string
		version string
		want    error
	}{
		"fine":                 {good, "24.1.0", nil},
		"inside the project":   {inside, "24.1.0", ErrRuntimeMissing},
		"relative":             {"./node", "24.1.0", ErrRuntimeMissing},
		"too old":              {good, "18.19.0", ErrRuntimeUnsupported},
		"version not a number": {good, "weird", ErrRuntimeUnsupported},
	} {
		t.Run(name, func(t *testing.T) {
			r := Resolver{Workspace: root, LookPath: func(string) (string, error) { return tc.path, nil }, Probe: fakeProbe(tc.version)}
			rt, err := r.Resolve(context.Background())
			if tc.want == nil {
				if err != nil || rt.Kind != "node" || rt.Path != good {
					t.Fatalf("runtime = %+v, %v", rt, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	r := Resolver{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrRuntimeMissing) {
		t.Fatalf("no node at all = %v, want ErrRuntimeMissing", err)
	}
}

func TestAProjectSymlinkToNodeDoesNotHideIt(t *testing.T) {
	root := testenv.TempDir(t)
	real := filepath.Join(root, "node")
	if err := writeExecutable(real); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(testenv.TempDir(t), "node")
	if err := symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	r := Resolver{Workspace: root, LookPath: func(string) (string, error) { return link, nil }, Probe: fakeProbe("24.0.0")}
	if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrRuntimeMissing) {
		t.Fatalf("a link into the project = %v, want it refused", err)
	}
}
