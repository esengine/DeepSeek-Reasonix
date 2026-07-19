package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"reasonix/internal/i18n"
	"reasonix/internal/node"
)

// runNode starts the multi-session Reasonix Node daemon for mobile remote mode.
// It is intentionally separate from single-session reasonix serve.
func runNode(args []string) int {
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8790", "listen address for the mobile WebSocket/HTTP API")
	nodeID := fs.String("id", "", "stable node identity (default: hostname-based)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	id := *nodeID
	if id == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "reasonix"
		}
		id = "node-" + host
	}
	hub := node.NewHub(id)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           hub.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("reasonix node — %s on http://%s\n", id, *addr)
	fmt.Printf("  mobile ws:  ws://%s/mobile/ws\n", *addr)
	fmt.Printf("  health:     http://%s/healthz\n", *addr)
	fmt.Printf("  note: single-session browser API remains `reasonix serve`\n")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return 0
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		return 0
	}
}
