package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"reasonix/desktop/internal/hostrpc"
	"reasonix/desktop/internal/instanceidentity"
	"reasonix/internal/config"
	"reasonix/internal/extension/rpcwire"
)

const (
	hostRPCFlag      = "--host-rpc"
	emitContractFlag = "-emit-contract"
	contractTSFile   = "desktopContract.generated.ts"
	contractJSONFile = "desktopContract.generated.json"

	desktopWindowMinWidth       = 760
	desktopWindowMinHeight      = 480
	hostDetachedShutdownTimeout = 15 * time.Second
)

type detachedShutdownResult struct {
	status shutdownStatus
	err    error
}

func hostRPCRequested(args []string) bool { return slices.Contains(args, hostRPCFlag) }

// exitIfHostLaunchMode runs -emit-contract or --host-rpc and exits with its
// code; any other launch returns to the caller.
func exitIfHostLaunchMode(args []string) {
	if dir, ok := emitContractDir(args); ok {
		os.Exit(runEmitContract(dir))
	}
	if hostRPCRequested(args) {
		os.Exit(runHostRPC(NewApp(), os.Stdin, os.Stdout))
	}
}

// emitContractDir returns the directory named by -emit-contract <dir> or
// -emit-contract=<dir>; a leading double dash is accepted too.
func emitContractDir(args []string) (string, bool) {
	for i, arg := range args {
		name := "-" + strings.TrimLeft(arg, "-")
		if name == emitContractFlag && i+1 < len(args) {
			return args[i+1], true
		}
		if dir, ok := strings.CutPrefix(name, emitContractFlag+"="); ok && dir != "" {
			return dir, true
		}
	}
	return "", false
}

// emitContract writes the TypeScript and JSON contract files into dir from
// the App type alone; no App instance or runtime is constructed.
func emitContract(dir string) error {
	return emitHostContract(dir, dir)
}

func emitHostContract(dir, ownersDir string) error {
	owners, err := hostrpc.SourceOwnership(".", "App")
	if err != nil {
		return err
	}
	registry, err := hostrpc.NewRegistryWithOwners((*App)(nil), nil, owners)
	if err != nil {
		return err
	}
	contract := hostrpc.Build(registry, hostEventNames)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var ts, js bytes.Buffer
	if err := hostrpc.WriteTypeScript(&ts, contract); err != nil {
		return err
	}
	if err := hostrpc.WriteJSON(&js, contract); err != nil {
		return err
	}
	ownerJSON, err := json.MarshalIndent(owners, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ownersDir, hostCommandOwnersFile), append(ownerJSON, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, contractTSFile), ts.Bytes(), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, contractJSONFile), js.Bytes(), 0o644)
}

func runEmitContract(dir string) int {
	if err := emitHostContract(dir, "."); err != nil {
		fmt.Fprintln(os.Stderr, "emit-contract:", err)
		return 1
	}
	return 0
}

// runHostRPC serves the desktop host protocol over stdin/stdout until the
// shell closes stdin or acknowledges desktop/shutdown. It returns the
// process exit code.
func runHostRPC(app *App, stdin io.Reader, stdout io.Writer) int {
	stopEndpoint, err := startUpdateEndpoint()
	if err != nil {
		slog.Error("desktop host: update endpoint", "err", err)
		return 2
	}
	defer stopEndpoint()
	registry, err := newDesktopRegistry(app)
	if err != nil {
		slog.Error("desktop host: contract registry", "err", err)
		return 2
	}
	// Lifecycle evidence and the probationary-update identity are claimed
	// before any request, before the shell connects.
	prepareDesktopDiagnostics(app)
	capturePendingUpdateHealthIdentity(app)
	defer app.releaseDesktopDiagnosticsOwnership()
	shutdownAttempted := false
	defer func() {
		if shutdownAttempted || app.shutdownStatus("").Completed {
			return
		}
		status, shutdownErr := requestDetachedShutdown(app, shutdownRequest{
			RequestID: newDesktopLifecycleRunID(), Reason: shutdownReasonStartupFailure,
		}, hostDetachedShutdownTimeout)
		if shutdownErr != nil || !status.Completed {
			slog.Error("desktop host: startup-failure cleanup failed", "err", shutdownErr, "phase", status.Phase)
		}
	}()
	generation, err := randomHex(8)
	if err != nil {
		slog.Error("desktop host: runtime generation", "err", err)
		return 2
	}
	token, err := randomHex(32)
	if err != nil {
		slog.Error("desktop host: resource token", "err", err)
		return 2
	}
	origin, stopOrigin, err := startResourceOrigin(app, token)
	if err != nil {
		slog.Error("desktop host: resource origin", "err", err)
		return 2
	}
	defer stopOrigin()

	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hostCtx, stopSignals := signal.NotifyContext(appCtx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	conn := rpcwire.NewConn(stdin, stdout, rpcwire.Options{
		StrictJSONRPC:         true,
		MaxInboundBytes:       64 << 20,
		MaxOutboundBytes:      64 << 20,
		MaxConcurrentHandlers: 512,
		MaxWriteStall:         30 * time.Second,
		Name:                  "desktop-host",
	})
	bridge := &hostShellBridge{app: app}
	server := hostrpc.NewServer(conn, hostrpc.ServerConfig{
		Registry:   registry,
		Contract:   hostrpc.Build(registry, hostEventNames),
		Hooks:      hostRPCHooks(appCtx, app, bridge, hostrpc.Resources{Origin: origin, Token: token}),
		Identity:   hostIdentity(),
		Generation: "g-" + generation,
	})
	bridge.server = server
	app.hostShell = bridge
	app.setNativeHost(rpcNativeHost{server: server})
	runtimeEventsEmitFallback = func(_ context.Context, name string, payload ...any) {
		server.Emit(name, payload...)
	}
	if err := server.Serve(hostCtx); err != nil {
		reason := shutdownReasonConnectionLost
		if hostCtx.Err() != nil && appCtx.Err() == nil {
			reason = shutdownReasonSystemSignal
		}
		slog.Error("desktop host: connection ended", "err", err)
		shutdownAttempted = true
		status, shutdownErr := requestDetachedShutdown(app, shutdownRequest{
			RequestID: newDesktopLifecycleRunID(), Reason: reason,
		}, hostDetachedShutdownTimeout)
		if shutdownErr != nil || !status.Completed {
			slog.Error("desktop host: connection-loss cleanup failed", "err", shutdownErr, "phase", status.Phase)
		}
		return 1
	}
	if !app.shutdownStatus("").Completed {
		shutdownAttempted = true
		status, shutdownErr := requestDetachedShutdown(app, shutdownRequest{
			RequestID: newDesktopLifecycleRunID(), Reason: shutdownReasonConnectionLost,
		}, hostDetachedShutdownTimeout)
		if shutdownErr != nil || !status.Completed {
			slog.Error("desktop host: EOF cleanup failed", "err", shutdownErr, "phase", status.Phase)
			return 1
		}
	}
	return 0
}

// requestDetachedShutdown bounds cleanup only after the shell transport or OS
// signal is already gone. Explicit user shutdown remains unbounded and
// retryable because its window is still available to surface a save failure.
func requestDetachedShutdown(app *App, request shutdownRequest, timeout time.Duration) (shutdownStatus, error) {
	result := make(chan detachedShutdownResult, 1)
	go func() {
		status, err := app.requestShutdown(context.Background(), request)
		result <- detachedShutdownResult{status: status, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.status, completed.err
	case <-timer.C:
		status := app.shutdownStatus("")
		reason := status.Reason
		if reason == "" {
			reason = normalizeShutdownReason(request.Reason)
		}
		app.lifecycle.tracker.markShutdown(reason, status.Phase, "interrupted")
		return status, fmt.Errorf("detached shutdown timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
}

// hostRPCHooks binds the shell's lifecycle requests to the App hooks, always
// with the service-lifetime context the App stores.
func hostRPCHooks(ctx context.Context, app *App, bridge *hostShellBridge, resources hostrpc.Resources) hostrpc.Hooks {
	return hostrpc.Hooks{
		Hello: func(hostrpc.HelloParams) (hostrpc.HelloResult, error) {
			runID := ""
			if app.lifecycle.tracker != nil {
				runID = app.lifecycle.tracker.state.RunID
			}
			return hostrpc.HelloResult{
				Resources: resources, Window: initialDesktopWindowGeometry(),
				RunID: runID, IncidentID: app.lifecycle.tracker.incidentID(),
				DiagnosticsEnabled: app.diagnosticsTelemetry,
			}, nil
		},
		Start:    func(context.Context) error { app.startup(ctx); return nil },
		DOMReady: func(context.Context) error { app.domReady(ctx); return nil },
		RendererAttached: func(context.Context, int) error {
			app.ReportDesktopWebViewReady()
			return nil
		},
		BeforeClose: func(_ context.Context, reason string) bool { return bridge.beforeClose(ctx, reason) },
		Shutdown: func(requestCtx context.Context, params hostrpc.ShutdownParams) (hostrpc.ShutdownResult, error) {
			status, err := app.requestShutdown(requestCtx, shutdownRequest{RequestID: params.RequestID, Reason: params.Reason})
			return hostRPCShutdownResult(status), err
		},
		ShutdownStatus: func(_ context.Context, params hostrpc.ShutdownStatusParams) (hostrpc.ShutdownResult, error) {
			return hostRPCShutdownResult(app.shutdownStatus(params.RequestID)), nil
		},
		HostEvent: bridge.handleHostEvent,
		BrowserControl: func(_ context.Context, enabled bool) error {
			app.setBrowserControlEnabled(enabled)
			return nil
		},
	}
}

func hostRPCShutdownResult(status shutdownStatus) hostrpc.ShutdownResult {
	return hostrpc.ShutdownResult{
		RequestID: status.RequestID, Reason: status.Reason, Phase: status.Phase, Outcome: status.Outcome,
		Completed: status.Completed, Retryable: status.Retryable, ErrorCode: status.ErrorCode,
		Error: status.Error, UpdatedAt: status.UpdatedAt,
	}
}

func hostIdentity() hostrpc.Identity {
	return hostrpc.Identity{
		Version: version,
		Channel: channel,
		Commit:  buildCommit(),
		Home:    instanceidentity.AccessHome(config.ReasonixHomeDir()),
	}
}

func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

// startResourceOrigin serves the authorised asset middlewares on a loopback
// port behind a bearer token; anything they do not claim is a 404.
func startResourceOrigin(app *App, token string) (origin string, stop func(), err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	handler := http.NotFoundHandler()
	chain := []func(http.Handler) http.Handler{
		app.attachmentUploadMiddleware(),
		app.jsProfilingMiddleware(),
		app.remoteMarkdownImageMiddleware(),
		app.workspaceMediaMiddleware(),
		app.themeAssetMiddleware(),
	}
	for _, middleware := range slices.Backward(chain) {
		handler = middleware(handler)
	}
	server := &http.Server{Handler: bearerAuth(token, handler), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("desktop host: resource origin stopped", "err", err)
		}
	}()
	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String(), stop, nil
}

func bearerAuth(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
