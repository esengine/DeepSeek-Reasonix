package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestEnvironmentFencePreservesWALAndSurvivesMaintenance(t *testing.T) {
	path, _ := leaseTestPath(t)
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "original"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(SessionEventLogPath(path))
	meta, _, _ := LoadBranchMeta(path)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	auth, _ := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	binding := ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "g", SourceWorkspace: t.TempDir(), WorkspaceRoot: t.TempDir(), WorkingCopyRoot: t.TempDir()}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(SessionEventLogPath(path))
	line, tail, ok := bytes.Cut(after, []byte("\n"))
	var header struct {
		SchemaVersion int    `json:"schema_version"`
		Type          string `json:"type"`
	}
	if !ok || json.Unmarshal(line, &header) != nil || header.SchemaVersion != 6 || header.Type != "execution_environment" || !bytes.Equal(tail, before) {
		t.Fatalf("missing lossless downgrade fence: %s", after)
	}
	current, _, _ := LoadBranchMeta(path)
	if current.Revision != meta.Revision || current.ContentDigest != meta.ContentDigest || !current.CreatedAt.Equal(meta.CreatedAt) {
		t.Fatal("binding changed transcript ledger")
	}
	if err := os.Remove(ExecutionEnvironmentPath(path)); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := LoadExecutionEnvironment(path); err != nil || !bound {
		t.Fatalf("fence not authoritative: %v %v", bound, err)
	}
	s.BindWriteAuthority(auth)
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "next"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(SessionEventLogPath(path))
	if !bytes.HasPrefix(after, append(line, '\n')) {
		t.Fatal("maintenance removed downgrade fence")
	}
	loaded, err := LoadSession(path)
	if err != nil || len(loaded.Snapshot()) != len(s.Snapshot()) {
		t.Fatalf("replay lost content: %v", err)
	}
}

func boundFenceFixture(t *testing.T) (string, *SessionWriteAuthority, ExecutionEnvironmentBinding) {
	t.Helper()
	path, _ := leaseTestPath(t)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	binding := ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "g", SourceWorkspace: t.TempDir(), WorkspaceRoot: t.TempDir(), WorkingCopyRoot: t.TempDir()}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	return path, auth, binding
}

func TestEnvironmentFenceRejectsCorruptionAndRetainsUnknownFields(t *testing.T) {
	for _, broken := range []string{`{"schema_version":6,"type":"execution_environment"}`, `{"schema_version":6,`, `{"schema_version":6,"type":"execution_environment","binding":{}}`} {
		t.Run(broken, func(t *testing.T) {
			path, _, _ := boundFenceFixture(t)
			if err := os.WriteFile(SessionEventLogPath(path), []byte(broken), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSession(path); !errors.Is(err, ErrExecutionEnvironmentProtocol) {
				t.Fatalf("checkpoint fallback: %v", err)
			}
			if _, _, err := LoadExecutionEnvironment(path); !errors.Is(err, ErrExecutionEnvironmentProtocol) {
				t.Fatalf("binding fallback: %v", err)
			}
		})
	}
	path, auth, binding := boundFenceFixture(t)
	raw, _ := os.ReadFile(SessionEventLogPath(path))
	raw = bytes.Replace(raw, []byte(`"binding":{`), []byte(`"future_envelope":{"keep":true},"binding":{"future_binding":123,`), 1)
	if err := os.WriteFile(SessionEventLogPath(path), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	s := NewSession("system")
	s.BindWriteAuthority(auth)
	if err := s.SaveRewriteCompact(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(SessionEventLogPath(path))
	if !bytes.HasPrefix(got, raw) {
		t.Fatal("unknown fence fields lost")
	}
}

func TestEnvironmentFenceBindingFailureIsAtomicAndResumable(t *testing.T) {
	path, auth, binding := boundFenceFixture(t)
	if err := os.Remove(SessionEventLogPath(path)); err != nil {
		t.Fatal(err)
	}
	old := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = old }()
	fileutil.CrashPoint = func(op, p string) {
		if op == "atomic-write" && CanonicalSessionPath(p) == CanonicalSessionPath(SessionEventLogPath(path)) {
			panic("interrupted fence")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("fault not reached")
			}
		}()
		_ = BindExecutionEnvironment(path, binding, auth)
	}()
	fileutil.CrashPoint = old
	if _, err := os.Stat(SessionEventLogPath(path)); !os.IsNotExist(err) {
		t.Fatalf("partial fence visible: %v", err)
	}
	if err := EnsureExecutionEnvironmentFence(path, auth); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ExecutionEnvironmentPath(path)); err != nil {
		t.Fatal(err)
	}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ExecutionEnvironmentPath(path)); err != nil {
		t.Fatal("derived binding not repaired", err)
	}
}

func TestEnvironmentFenceBlocksUnownedSaveAndUnsafeCopies(t *testing.T) {
	path, auth, binding := boundFenceFixture(t)
	s := NewSession("system")
	s.BindWriteAuthority(auth)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := NewSession("stranger").Save(path); !errors.Is(err, ErrSessionWriteAuthorityMissing) {
		t.Fatalf("unowned save: %v", err)
	}
	if _, err := ExportNativeArchive(path); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("archive lost constraint: %v", err)
	}
	if _, err := MoveSession(path, t.TempDir(), t.TempDir()); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("move lost constraint: %v", err)
	}
	if _, err := AdoptConversation(t.TempDir(), AdoptInput{Record: AdoptRecord{From: path}}); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("adopt lost constraint: %v", err)
	}
	version, err := SaveSupersededVersion(path, s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(version); err != nil {
		t.Fatal(err)
	}
	if err := CheckExecutionEnvironment(version, binding.WorkspaceRoot); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("historical copy executable: %v", err)
	}
}

func TestEnvironmentFenceLockedRecoveryAndRetiredPublication(t *testing.T) {
	path, _, binding := boundFenceFixture(t)
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "retained"})
	var retired *SessionPublication
	info, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: path, BeforePublishLocked: func(target string, p *SessionPublication) error {
		retired = p
		lease, err := TryAcquireSessionLease(target)
		if err != nil {
			return err
		}
		defer lease.Release()
		auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
		if err != nil {
			return err
		}
		return p.BindExecutionEnvironment(binding, auth)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, bound, err := LoadExecutionEnvironment(info.Path); err != nil || !bound {
		t.Fatalf("recovery unfenced: %v %v", bound, err)
	}
	if err := retired.BindExecutionEnvironment(binding, nil); !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("retired token: %v", err)
	}
}

func TestEnvironmentFenceActualPreviousReader(t *testing.T) {
	command := os.Getenv("REASONIX_PREVIOUS_SESSION_READER")
	if command == "" {
		t.Skip("previous-version executable supplied by compatibility qualification")
	}
	path, auth, _ := boundFenceFixture(t)
	s := NewSession("system")
	s.BindWriteAuthority(auth)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "keep"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	for _, maintenance := range []bool{false, true} {
		if maintenance {
			if err := s.SaveRewriteCompact(path); err != nil {
				t.Fatal(err)
			}
		}
		before := map[string][]byte{}
		files, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil {
				before[f] = b
			}
		}
		cmd := exec.Command(command, "-test.run=^TestPreviousVersionRejectsEnvironmentFence$", "-test.v")
		cmd.Env = append(os.Environ(), "REASONIX_FENCED_SESSION="+path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("previous-version reader/writer: %v\n%s", err, out)
		}
		t.Log(string(out))
		for f, want := range before {
			got, err := os.ReadFile(f)
			if err != nil || !bytes.Equal(want, got) {
				t.Fatalf("previous writer changed %s: %v", f, err)
			}
		}
	}
}

func TestEnvironmentFencePreservesCompositionImagesAndRepairsTail(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			path, auth, _ := boundFenceFixture(t)
			d := agentcomposition.Builtins()[0]
			binding := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d}
			if live {
				binding.FormatVersion, binding.ResourceMode = 2, "live"
			}
			binding.Seal()
			s := NewSession("system")
			s.BindComposition(binding)
			s.BindWriteAuthority(auth)
			image := testImageDataURL(17, 1200)
			s.Add(provider.Message{Role: provider.RoleUser, Content: "keep", Images: []string{image}})
			if err := s.Save(path); err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(SessionEventLogPath(path), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString(`{"schema_version":1,"type":"append"`)
			if closeErr := f.Close(); err != nil || closeErr != nil {
				t.Fatal(errors.Join(err, closeErr))
			}
			if err := s.SaveRewriteCompact(path); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.CompositionSummary().Digest != binding.Digest || loaded.Snapshot()[1].Images[0] != image {
				t.Fatal("maintenance changed composition or image")
			}
			stripped := NewSession("stripped")
			stripped.BindWriteAuthority(auth)
			if err := stripped.SaveRewrite(path); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(SessionEventLogPath(path))
			if err != nil || bytes.Contains(raw, []byte(`"agent_composition":`)) || !bytes.Contains(raw, []byte(`"type":"execution_environment"`)) {
				t.Fatalf("composition removal did not compact while retaining environment: %v", err)
			}
		})
	}
}

func TestEnvironmentFenceRefusesUnownedFormatsWithoutChangingBytes(t *testing.T) {
	for _, old := range []string{`{"schema_version":2,"type":"replace"}`, `{"schema_version":999,"type":"replace"}`, `{"foreign":true}`} {
		t.Run(old, func(t *testing.T) {
			path, auth, binding := boundFenceFixture(t)
			if err := os.WriteFile(SessionEventLogPath(path), []byte(old), 0600); err != nil {
				t.Fatal(err)
			}
			if err := BindExecutionEnvironment(path, binding, auth); !errors.Is(err, ErrExecutionEnvironmentProtocol) {
				t.Fatalf("unsupported format rebound: %v", err)
			}
			got, _ := os.ReadFile(SessionEventLogPath(path))
			if string(got) != old {
				t.Fatal("unsupported format overwritten")
			}
		})
	}
}

func TestEnvironmentFenceReservationRequiresItsLiveAuthority(t *testing.T) {
	path, _ := leaseTestPath(t)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	auth, _ := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	binding := ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "g", SourceWorkspace: t.TempDir(), WorkspaceRoot: t.TempDir(), WorkingCopyRoot: t.TempDir()}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	if err := NewSession("stranger").SaveIfAbsent(path); !os.IsExist(err) {
		t.Fatalf("reservation stolen: %v", err)
	}
	auth, _ = lease.IssueWriteAuthority(NextSessionWriteGeneration())
	stranger := NewSession("another generation")
	stranger.BindWriteAuthority(auth)
	if err := stranger.SaveIfAbsent(path); !os.IsExist(err) {
		t.Fatalf("unbound authority stole reservation: %v", err)
	}
	if err := BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	s := NewSession("owner")
	s.BindWriteAuthority(auth)
	if err := s.SaveIfAbsent(path); err != nil {
		t.Fatalf("owner cannot publish reserved transcript: %v", err)
	}
}
