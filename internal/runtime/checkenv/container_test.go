package checkenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/shellrun"
)

func containerFixture(t *testing.T) Spec {
	t.Helper()
	image := os.Getenv("REASONIX_TEST_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("requires a locally provisioned immutable verification image and Docker Engine")
	}
	source, storage := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "input"), []byte("frozen\n"), 0644); err != nil {
		t.Fatal(err)
	}
	client, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	var environment []string
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "PATH", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	return Spec{RuntimeRoot: "docker-image://" + image, SourceRoot: source, Directory: source, StorageRoot: storage, Client: ClientSpec{Executable: client, Environment: environment}}
}

func TestContainerControlledReceipt(t *testing.T) {
	spec := containerFixture(t)
	t.Setenv("REASONIX_HOST_SECRET", "must-not-inherit")
	command := `[[ $(< input) == frozen && -z ${REASONIX_HOST_SECRET:-} && $EVIDENCE_VALUE == declared && $UID == 65534 ]] || exit 41
if printf overwrite > input 2>/dev/null; then exit 42; fi
[[ ! -e /var/run/docker.sock && ! -e /run/reasonix-control/status ]] || exit 43
printf controlled`
	i := NewEnvironment(spec, command, "container-execution", "container-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory, DeclaredEnvironment: map[string]string{"EVIDENCE_VALUE": "declared"}})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true})
	if result.Err != nil || !strings.Contains(result.Combined, "controlled") {
		t.Fatalf("controlled container: %v %s", result.Err, result.Combined)
	}
	inputs := i.Inputs()
	if len(inputs) != 1 || !inputs[0].Stable || !inputs[0].HasLaunchIdentity(spec.Directory) || !Verify(inputs[0], "container-execution") {
		t.Fatalf("missing owned receipt: %+v", inputs)
	}
	if Verify(inputs[0], "other-execution") {
		t.Fatal("receipt crossed execution owner")
	}
	i.Close()
	if !Verify(inputs[0], "container-execution") {
		t.Fatal("cleanup invalidated immutable receipt")
	}
	if body, err := os.ReadFile(filepath.Join(spec.SourceRoot, "input")); err != nil || string(body) != "frozen\n" {
		t.Fatalf("host input changed: %q %v", body, err)
	}
}

func TestContainerCancellationCannotMintReceipt(t *testing.T) {
	spec := containerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	command := `printf started; while :; do :; done`
	i := NewEnvironment(spec, command, "cancel-execution", "cancel-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(ctx, tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan shellrun.Result, 1)
	started := make(chan struct{})
	var once sync.Once
	go func() {
		done <- shellrun.RunForeground(tool.WithControlledShell(ctx, i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true, Progress: func(body string) {
			if strings.Contains(body, "started") {
				once.Do(func() { close(started) })
				cancel()
			}
		}})
	}()
	select {
	case result := <-done:
		select {
		case <-started:
		default:
			t.Fatalf("payload never started: %v %s", result.Err, result.Combined)
		}
		if result.Err == nil {
			t.Fatal("cancelled check succeeded")
		}
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("container cancellation did not close the process scope")
	}
	for _, input := range i.Inputs() {
		if Verify(input, "cancel-execution") {
			t.Fatal("cancelled check minted a receipt")
		}
	}
}

func TestContainerImageIdentityRequiresDigest(t *testing.T) {
	for _, value := range []string{"latest", "sha256:short", strings.Repeat("a", 64), "sha256:" + strings.Repeat("g", 64), "sha256:" + strings.Repeat("a", 64) + "?pull=true"} {
		if _, err := containerImageID("docker-image://" + value); err == nil {
			t.Fatalf("accepted mutable or invalid image %q", value)
		}
	}
}

func TestContainerConfigurationPreservesEffectiveOomAndOtherPolicy(t *testing.T) {
	before := containerInspection{HostConfig: []byte(`{"OomKillDisable":false,"ReadonlyRootfs":true}`)}
	after := containerInspection{HostConfig: []byte(`{"OomKillDisable":null,"ReadonlyRootfs":true}`)}
	if before.configuration() != after.configuration() {
		t.Fatal("zero OOM policy changed at start")
	}
	for _, policy := range []string{`{"OomKillDisable":true,"ReadonlyRootfs":true}`, `{"OomKillDisable":null,"ReadonlyRootfs":false}`, `{"OomKillDisable":null,"ReadonlyRootfs":true,"UnknownFutureAuthority":true}`} {
		after.HostConfig = []byte(policy)
		if before.configuration() == after.configuration() {
			t.Fatal("effective or unknown policy change was ignored")
		}
	}
}

func TestContainerEndpointAcceptsOnlyUnambiguousLocalTransport(t *testing.T) {
	for _, endpoint := range []string{"tcp://127.0.0.1:2375", "ssh://user@host", "unix://other-machine/tmp/engine.sock", "unix:///tmp/engine.sock?network=host", "npipe:////server/pipe/docker_engine", "npipe:////./pipe/../docker_engine"} {
		if _, err := containerEndpoint(t.Context(), "", t.TempDir(), []string{"DOCKER_HOST=" + endpoint}); err == nil {
			t.Fatalf("accepted external or ambiguous endpoint %q", endpoint)
		}
	}
	endpoint := "unix:///tmp/engine.sock"
	if runtime.GOOS == "windows" {
		endpoint = "npipe:////./pipe/docker_engine"
	}
	if _, err := containerEndpoint(t.Context(), "", t.TempDir(), []string{"DOCKER_HOST=" + endpoint}); err != nil {
		t.Fatal(err)
	}
	if _, err := containerEndpoint(t.Context(), "", t.TempDir(), []string{"DOCKER_HOST=" + endpoint, "DOCKER_CONTEXT=other"}); err == nil {
		t.Fatal("accepted competing host/context declarations")
	}
}
