package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/session/control"
)

func TestServeKeepsLaunchTokenOffTheTerminal(t *testing.T) {
	for _, mode := range []string{"none", "token"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", testenv.TempDir(t))
			ctrl := control.New(control.Options{SessionDir: testenv.TempDir(t)})
			t.Cleanup(ctrl.Close)
			srv := serve.New(ctrl, serve.NewBroadcaster(), config.ServeConfig{AuthMode: mode})
			resources := &serveFrontendResources{}
			opts := serveFrontendOptions{command: "serve"}
			path, err := launchTokenLocation(srv.AuthToken(), opts, resources)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.release(false)
			if filepath.Dir(path) != config.RemoteStateDir() {
				t.Fatalf("launch token file %s is outside the sandbox-denied %s", path, config.RemoteStateDir())
			}
			data, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(data)) != srv.AuthToken() {
				t.Fatalf("launch token file = %q, %v", data, err)
			}
			if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
				t.Fatalf("launch token file mode = %v, %v; want 0600", info.Mode().Perm(), err)
			}
			var report strings.Builder
			reportServeAuth(&report, srv.AuthMode(), "http://127.0.0.1:8787", path)
			out := report.String()
			if strings.Contains(out, srv.AuthToken()) || !strings.Contains(out, path) {
				t.Fatalf("serve report printed the token or omitted its file:\n%s", out)
			}
			resources.release(false)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("launch token file outlived the serve: %v", err)
			}
		})
	}
}

func TestManagedServePointsAtItsTokenFile(t *testing.T) {
	resources := &serveFrontendResources{}
	path, err := launchTokenLocation("secret", serveFrontendOptions{tokenFile: "/run/serve.token"}, resources)
	if err != nil || path != "/run/serve.token" || len(resources.artifacts) != 0 {
		t.Fatalf("managed launch token location = %q %v artifacts=%v", path, err, resources.artifacts)
	}
}
