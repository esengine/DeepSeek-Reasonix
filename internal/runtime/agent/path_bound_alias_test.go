package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/tools/builtin"
)

type uncertainBoundWriter struct {
	recordingWriter
	paths []string
}

func (w *uncertainBoundWriter) WritePaths(json.RawMessage) ([]string, error) {
	return w.paths, fileutil.ErrAmbiguousPath
}

func TestBoundWriterChecksResolvedGrantDespiteLeaseAmbiguity(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	inner := &uncertainBoundWriter{recordingWriter: recordingWriter{name: "write_file", writesPaths: true}, paths: []string{filepath.Join(root, "target")}}
	w := pathBoundWriter{inner: inner, grant: writeclaim.NewWriteGrant(claim), sched: writeclaim.NewSubagentScheduler(4, 2), workDir: root}
	if _, err := w.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("resolved authorized target: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("calls=%d", inner.calls)
	}
	inner.paths = []string{filepath.Join(testenv.TempDir(t), "outside")}
	if _, err := w.Execute(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, writeclaim.ErrWriteFenceClosed) {
		t.Fatalf("outside target: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("outside write executed: calls=%d", inner.calls)
	}
}

func TestBoundWriterWindowsAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows aliases")
	}
	for _, suffix := range []string{".", " "} {
		for _, allowed := range []bool{true, false} {
			t.Run(suffix+map[bool]string{true: "inside", false: "outside"}[allowed], func(t *testing.T) {
				root := testenv.TempDir(t)
				grantRoot := root
				if !allowed {
					grantRoot = filepath.Join(root, "granted")
					if err := os.Mkdir(grantRoot, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				writer := aliasBoundWriter(t, root, grantRoot, "write_file")
				args := mustJSON(t, map[string]string{"path": filepath.Join(root, "fixture") + suffix, "content": "fixture"})
				assertAliasWrite(t, writer, args, filepath.Join(root, "fixture"), allowed)
			})
		}
	}
}

func TestBoundWriterLeafSymlinkGrant(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "inside", false: "outside"}[allowed], func(t *testing.T) {
			root, outside := testenv.TempDir(t), testenv.TempDir(t)
			target := filepath.Join(root, "target")
			if !allowed {
				target = filepath.Join(outside, "target")
			}
			if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			writer := aliasBoundWriter(t, root, root, "write_file")
			assertAliasWrite(t, writer, mustJSON(t, map[string]string{"path": link, "content": "fixture"}), target, allowed)
		})
	}
}

func TestBoundMoveChecksBothAliasEndpoints(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows aliases")
	}
	for _, outsideSource := range []bool{true, false} {
		t.Run(map[bool]string{true: "source", false: "destination"}[outsideSource], func(t *testing.T) {
			root := testenv.TempDir(t)
			granted := filepath.Join(root, "granted")
			if err := os.Mkdir(granted, 0o700); err != nil {
				t.Fatal(err)
			}
			src, dst := filepath.Join(granted, "source"), filepath.Join(root, "destination")
			if outsideSource {
				src, dst = filepath.Join(root, "source"), filepath.Join(granted, "destination")
			}
			if err := os.WriteFile(src, []byte("prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			writer := aliasBoundWriter(t, root, granted, "move_file")
			_, err := writer.Execute(context.Background(), mustJSON(t, map[string]string{"source_path": src + ".", "destination_path": dst + "."}))
			if !errors.Is(err, writeclaim.ErrWriteFenceClosed) {
				t.Fatalf("outside endpoint: %v", err)
			}
			if data, err := os.ReadFile(src); err != nil || string(data) != "prior" {
				t.Fatalf("source changed: %q %v", data, err)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("destination created: %v", err)
			}
		})
	}
}

func TestBoundMoveAllowedAliasEndpoints(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows aliases")
	}
	root := testenv.TempDir(t)
	src, dst := filepath.Join(root, "source"), filepath.Join(root, "destination")
	if err := os.WriteFile(src, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := aliasBoundWriter(t, root, root, "move_file")
	_, err := writer.Execute(context.Background(), mustJSON(t, map[string]string{"source_path": src + ".", "destination_path": dst + "."}))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "prior" {
		t.Fatalf("move effect: %q %v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source retained: %v", err)
	}
}

func aliasBoundWriter(t *testing.T, root, grantRoot, name string) tool.Tool {
	t.Helper()
	claim, err := writeclaim.NormalizeWritePaths(root, []string{grantRoot})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, writer := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		reg.Add(writer)
	}
	bound, _ := BindWritePaths(reg, writeclaim.NewWriteGrant(claim), nil, writeclaim.NewSubagentScheduler(4, 2), root, false)
	return mustGet(t, bound, name)
}

func assertAliasWrite(t *testing.T, writer tool.Tool, args []byte, target string, allowed bool) {
	t.Helper()
	prior, priorErr := os.ReadFile(target)
	if priorErr != nil && !os.IsNotExist(priorErr) {
		t.Fatal(priorErr)
	}
	_, err := writer.(tool.WritePathResolver).WritePaths(args)
	if !errors.Is(err, fileutil.ErrAmbiguousPath) {
		t.Fatalf("lease must remain conservative: %v", err)
	}
	_, err = writer.Execute(context.Background(), args)
	if allowed {
		if err != nil {
			t.Fatalf("authorized write: %v", err)
		}
		if data, err := os.ReadFile(target); err != nil || string(data) != "fixture" {
			t.Fatalf("write effect: %q %v", data, err)
		}
	} else {
		if !errors.Is(err, writeclaim.ErrWriteFenceClosed) {
			t.Fatalf("outside write: %v", err)
		}
		data, err := os.ReadFile(target)
		if (priorErr == nil && err != nil) || (os.IsNotExist(priorErr) && !os.IsNotExist(err)) || string(data) != string(prior) {
			t.Fatalf("outside target changed: %q %v", data, err)
		}
	}
}
