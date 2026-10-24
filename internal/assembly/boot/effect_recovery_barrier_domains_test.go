package boot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/gitcmd"
	"slices"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/control"
)

func recoveryDomainRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	writeFile(t, root, "a.txt", "original a")
	writeFile(t, root, "b.txt", "original b")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	return root
}

func TestRecoveryDiscoveryAcceptsFileAndFutureDirectoryWriteRoots(t *testing.T) {
	root := recoveryDomainRepo(t)
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "a.txt"), filepath.Join(root, "future", "nested")} {
		repos, resources, err := mutationRepositories(t.Context(), repo, []string{path})
		if err != nil || len(repos) != 1 || !slices.Contains(resources, path) {
			t.Fatalf("write root discovery: %s %+v %v", path, repos, err)
		}
	}
	cfg := &config.Config{}
	cfg.Sandbox.AllowWrite = []string{t.TempDir()}
	cfg.Sandbox.WorkspaceRoot = t.TempDir()
	roots := runtimeWriteRoots(Options{WorkspaceOnly: true}, cfg, root, []string{t.TempDir()})
	if !slices.Equal(roots, []string{root}) {
		t.Fatalf("isolated authority expanded: %v", roots)
	}
}

func recoveryBarrierBuild(t *testing.T, mode string, root string, extras []string, configure ...func(*Options)) (*builder, *control.Controller, *string, *sync.Mutex) {
	t.Helper()
	home := statedBootHome(t)
	kind := "recovery-barrier-" + t.Name()
	destination := root
	if len(extras) > 0 {
		destination = extras[0]
	}
	p := &pathLeaseProvider{path: filepath.Join(destination, "refused.txt"), written: make(chan struct{}), resume: make(chan struct{})}
	close(p.resume)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", fmt.Sprintf("default_model=\"fixture\"\n[agent]\ncollaboration=%q\n[codegraph]\nenabled=false\n[[providers]]\nname=\"fixture\"\nkind=%q\nmodel=\"fixture\"\n", mode, kind))
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	var mu sync.Mutex
	code := ""
	b := newBuilder()
	opts := Options{Home: home, WorkspaceRoot: root, AdditionalDirs: extras, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			mu.Lock()
			code = e.Tool.RefusalCode
			mu.Unlock()
		}
	})}
	for _, fn := range configure {
		fn(&opts)
	}
	result, err := b.build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(result.Controller.Close)
	result.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	return b, result.Controller, &code, &mu
}

func TestEffectRecoveryBarrierExtendsReusedLeaseAfterRebuild(t *testing.T) {
	root, extra := recoveryDomainRepo(t), recoveryDomainRepo(t)
	b, first, _, _ := recoveryBarrierBuild(t, "", root, nil)
	owner := first.WorkspaceLeaseOwner()
	opts := b.opts
	opts.WorkspaceLease, opts.Jobs, opts.AdditionalDirs = owner, first.JobManager(), []string{extra}
	next, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.WorkspaceLeaseOwner() != owner {
		t.Fatal("fixture did not reuse lease")
	}
	leaveInterruptedIntegration(t, extra)
	owner.BeginRun()
	defer owner.EndRun()
	if _, _, err := owner.HoldPaths(t.Context(), []string{filepath.Join(extra, "refused.txt")}); err == nil {
		t.Fatal("rebuild omitted newly authorized repository")
	}
	leaveInterruptedIntegration(t, root)
	if _, _, err := owner.HoldPaths(t.Context(), []string{filepath.Join(root, "refused.txt")}); err == nil {
		t.Fatal("rebuild removed original repository barrier")
	}
}

func TestEffectRecoveryBarrierCoversFinalSandboxWriteRoots(t *testing.T) {
	for _, mode := range []string{"", "shared"} {
		for _, field := range []string{"allow_write", "workspace_root"} {
			t.Run(mode+"/"+field, func(t *testing.T) {
				root, extra := recoveryDomainRepo(t), recoveryDomainRepo(t)
				_, controller, code, mu := recoveryBarrierBuild(t, mode, root, []string{extra}, func(opts *Options) {
					opts.AdditionalDirs = nil
					body, err := os.ReadFile(filepath.Join(root, "reasonix.toml"))
					if err != nil {
						t.Fatal(err)
					}
					value := fmt.Sprintf("%q", extra)
					if field == "allow_write" {
						value = "[" + value + "]"
					}
					writeUserConfigAt(t, opts.Home, string(body)+"\n[sandbox]\n"+field+"="+value+"\n")
				})
				leaveInterruptedIntegration(t, extra)
				_ = controller.Run(t.Context(), "create refused.txt")
				mu.Lock()
				got := *code
				mu.Unlock()
				if got != "isolation.recovery_required" {
					t.Fatalf("sandbox write domain bypassed recovery: %q", got)
				}
				if _, err := os.Stat(filepath.Join(extra, "refused.txt")); !os.IsNotExist(err) {
					t.Fatalf("write crossed barrier: %v", err)
				}
			})
		}
	}
}

func TestEffectRecoveryBarrierCoversRetainedLegacyClaimsAndExtraRepositories(t *testing.T) {
	for _, mode := range []string{"", "shared"} {
		for _, extra := range []bool{false, true} {
			t.Run(fmt.Sprintf("mode=%s/extra=%t", mode, extra), func(t *testing.T) {
				root := recoveryDomainRepo(t)
				destination := root
				var extras []string
				if extra {
					destination = recoveryDomainRepo(t)
					extras = []string{destination}
				}
				b, controller, code, mu := recoveryBarrierBuild(t, mode, root, extras)
				if mode == "" {
					b.session.lease.BeginRun()
					defer b.session.lease.EndRun()
					if err := b.session.lease.AcquirePaths(t.Context(), []string{filepath.Join(destination, "refused.txt")}); err != nil {
						t.Fatal(err)
					}
				}
				leaveInterruptedIntegration(t, destination)
				if mode == "" {
					repo, err := gitcmd.Open(t.Context(), destination)
					if err != nil {
						t.Fatal(err)
					}
					release, err := filelock.Acquire(t.Context(), filepath.Join(repo.GitDir, "reasonix-apply-v1", "active.json.lock"))
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := b.session.lease.HoldPaths(t.Context(), nil); err == nil {
						t.Fatal("active journal lock waived legacy recovery barrier")
					}
					release()
				}
				_ = controller.Run(t.Context(), "create refused.txt")
				mu.Lock()
				got := *code
				mu.Unlock()
				if got != "isolation.recovery_required" {
					t.Fatalf("write bypassed journal: %q", got)
				}
				if _, err := os.Stat(filepath.Join(destination, "refused.txt")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("write crossed recovery barrier: %v", err)
				}
				if mode == "" {
					if _, _, err := b.session.lease.HoldPaths(t.Context(), nil); err == nil {
						t.Fatal("opaque legacy call crossed recovery barrier")
					}
					b.session.lease.EndRun()
					controller.Close()
					if next, err := Build(t.Context(), b.opts); err == nil {
						next.Close()
						t.Fatal("legacy startup ignored existing journal")
					} else {
						var refusal tool.Refusal
						if !errors.As(err, &refusal) || refusal.Code != "isolation.recovery_required" {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}
