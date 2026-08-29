package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/servepool"
)

// ServePoolAddress returns the gateway listen address ("" when disabled).
func (a *App) ServePoolAddress() string {
	if a == nil {
		return ""
	}
	return a.gatewayAddr
}

// startServePool starts the single-entry remote gateway in front of a pool of
// lazily spawned per-project serve processes (#8983). It binds 127.0.0.1 only
// — the desktop's own tabs stay authoritative; remote clients operate through
// the pool (one token-protected entry, /p/<project>/... routing). Failures
// disable the gateway silently (e.g. port taken) and log a warning.
func (a *App) startServePool(ctx context.Context) {
	roots := projectRootsFromRegistry()
	mgr, err := servepool.NewManager(servepool.Config{ProjectRoots: roots})
	if err != nil {
		slog.Warn("servepool: disabled", "err", err)
		return
	}
	token := loadOrCreateGatewayToken()
	gw := servepool.NewGateway(mgr, token, a)
	port := gatewayPort()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		mgr.Close()
		slog.Warn("servepool: gateway listen failed", "port", port, "err", err)
		return
	}
	a.servePool = mgr
	a.gatewayAddr = ln.Addr().String()
	srv := &http.Server{Handler: gw}
	a.gatewaySrv = srv
	a.goSafe("servepool-gateway", func() { _ = srv.Serve(ln) })
	slog.Info("servepool: gateway ready", "addr", a.gatewayAddr)
}

// closeServePool stops the gateway and all pooled serves.
func (a *App) closeServePool() {
	if a.gatewaySrv != nil {
		_ = a.gatewaySrv.Close()
		a.gatewaySrv = nil
	}
	if a.servePool != nil {
		a.servePool.Close()
		a.servePool = nil
	}
	a.gatewayAddr = ""
}

func projectRootsFromRegistry() []string {
	f := loadProjectsFile()
	roots := make([]string, 0, len(f.Projects))
	for _, p := range f.Projects {
		if strings.TrimSpace(p.Root) != "" {
			roots = append(roots, p.Root)
		}
	}
	return roots
}

func loadOrCreateGatewayToken() string {
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

// gatewayPort picks the gateway port: REASONIX_GATEWAY_PORT env override or
// the default 18789 (the desktop's internal serve keeps 8787).
func gatewayPort() int {
	if v := os.Getenv("REASONIX_GATEWAY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return 18789
}

// HandoffSession implements servepool.HostHook (#8987): releases a
// desktop-held session to the requesting serve writer via a handoff
// reservation. The gateway only calls this for takeover-session 409s whose
// holder is this process (the desktop), so no cross-machine authorization is
// involved — the request already passed the gateway token gate.
func (a *App) HandoffSession(projectID, sessionName, targetWriterID string) error {
	if strings.TrimSpace(sessionName) == "" || strings.ContainsAny(sessionName, `/\`) {
		return fmt.Errorf("handoff: invalid session name")
	}
	root := ""
	for _, p := range projectRootsFromRegistry() {
		if servepool.WorkspaceSlug(p) == projectID {
			root = p
			break
		}
	}
	if root == "" {
		return fmt.Errorf("handoff: unknown project %q", projectID)
	}
	dir := config.ProjectSessionDir(root)
	if dir == "" {
		return fmt.Errorf("handoff: no session dir for project %q", projectID)
	}
	path := filepath.Join(dir, sessionName+".jsonl")
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) {
		return fmt.Errorf("handoff: path outside session dir")
	}
	lease, err := agent.TryReclaimCurrentProcessSessionLease(path)
	if err != nil {
		return fmt.Errorf("handoff: %w", err)
	}
	defer lease.Release()
	if err := lease.ReleaseForHandoff(strings.TrimSpace(targetWriterID)); err != nil {
		return fmt.Errorf("handoff: %w", err)
	}
	return nil
}
