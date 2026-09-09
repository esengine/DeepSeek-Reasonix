package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/servepool"
)

// runServePool hosts the single-entry remote gateway as a standalone CLI
// process (headless Linux/NAS via systemd, #8983 P1b). The gateway serves
// GET /manifest, POST /projects/open and /p/<id>/* routing exactly like the
// desktop-embedded host; project roots come from the same
// desktop-projects.json registry.
func runServePool(addr, tokenFile, token string) int {
	roots := readDesktopProjectRoots()
	mgr, err := servepool.NewManager(servepool.Config{ProjectRoots: roots})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	defer mgr.Close()

	gwToken := strings.TrimSpace(token)
	if gwToken == "" && tokenFile != "" {
		if b, err := os.ReadFile(tokenFile); err == nil {
			gwToken = strings.TrimSpace(string(b))
		}
	}
	if gwToken == "" {
		gwToken = loadOrCreateGatewayTokenCLI()
	}
	gw := servepool.NewGateway(mgr, gwToken)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	srv := &http.Server{Handler: gw}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	fmt.Printf("reasonix serve --pool on http://%s (%d projects)\n", ln.Addr().String(), len(roots))
	fmt.Println("gateway token file: <reasonix home>/gateway-token")
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	return 0
}

// readDesktopProjectRoots reads the desktop sidebar's project registry.
func readDesktopProjectRoots() []string {
	type project struct {
		Root string `json:"root"`
	}
	type projectFile struct {
		Projects []project `json:"projects"`
	}
	home := config.ReasonixHomeDir()
	var saved projectFile
	if data, err := os.ReadFile(filepath.Join(home, "desktop-projects.json")); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	roots := make([]string, 0, len(saved.Projects))
	seen := map[string]bool{}
	for _, p := range saved.Projects {
		root := filepath.Clean(strings.TrimSpace(p.Root))
		if root == "" || root == "." || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	return roots
}

// loadOrCreateGatewayTokenCLI reuses the same gateway token file as the
// desktop host so one token works across both hosts on the same machine.
func loadOrCreateGatewayTokenCLI() string {
	path := filepath.Join(config.MemoryUserDir(), "gateway-token")
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok
		}
	}
	tok := servepool.RandomToken()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(tok), 0o600)
	return tok
}
