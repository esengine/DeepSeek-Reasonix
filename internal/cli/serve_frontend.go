package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/serve"
)

// runServe exposes the controller's HTTP and SSE frontend.
func runServe(args []string) int {
	return runServeWithOptions(args, serveRunOptions{command: "serve"})
}

// runWeb adds local browser lifecycle behavior to the Serve frontend.
func runWeb(args []string) int {
	return runServeWithOptions(args, serveRunOptions{command: "web", openBrowser: true})
}

type serveRunOptions struct {
	command     string
	openBrowser bool
}

type serveFrontendOptions struct {
	command     string
	address     string
	portFile    string
	tokenFile   string
	pidFile     string
	openBrowser bool
	hasSession  bool
}

type serveFrontendResources struct {
	listener     net.Listener
	displayAddr  string
	artifacts    []string
	registration *webInstanceRegistration
}

func prepareServeFrontend(opts serveFrontendOptions) (_ *serveFrontendResources, err error) {
	resources := &serveFrontendResources{displayAddr: opts.address}
	defer func() {
		if err != nil {
			resources.release(true)
		}
	}()

	if opts.portFile != "" || opts.command == "web" || opts.openBrowser {
		if opts.command == "web" {
			resources.listener, err = listenWebWithPortRetry(opts.address)
		} else {
			resources.listener, err = net.Listen("tcp", opts.address)
		}
		if err != nil {
			return nil, err
		}
		resources.displayAddr = resources.listener.Addr().String()
		requested, selected := requestedPort(opts.address), requestedPort(resources.displayAddr)
		if opts.command == "web" && requested != 0 && requested != selected {
			fmt.Fprintf(os.Stderr, "  port %d is in use; using %d instead\n", requested, selected)
		}
		if opts.portFile != "" {
			if err = writeServeAddrFile(opts.portFile, resources.displayAddr); err != nil {
				return nil, err
			}
			resources.artifacts = append(resources.artifacts, opts.portFile)
		}
	}
	if opts.pidFile != "" {
		if err = writeServePidFile(opts.pidFile); err != nil {
			return nil, err
		}
		resources.artifacts = append(resources.artifacts, opts.pidFile)
	}
	if opts.command == "web" {
		resources.registration, err = registerWebInstance(config.ReasonixHomeDir(), resources.displayAddr)
		if err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func (r *serveFrontendResources) release(closeListener bool) {
	if closeListener && r.listener != nil {
		_ = r.listener.Close()
	}
	if r.registration != nil {
		r.registration.Release()
	}
	for _, path := range r.artifacts {
		_ = os.Remove(path)
	}
}

func runServeFrontend(ctrl *control.Controller, srv *serve.Server, cfg config.ServeConfig, opts serveFrontendOptions) int {
	resources, err := prepareServeFrontend(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer resources.release(false)
	srv.EnableProviderSetupForListener(resources.displayAddr)
	reportServeFrontend(ctrl, srv, cfg, resources.displayAddr, opts)
	startServeBalanceDiagnostics(ctrl)
	return serveFrontendLoop(ctrl, srv, resources, opts)
}

func reportServeFrontend(ctrl *control.Controller, srv *serve.Server, cfg config.ServeConfig, address string, opts serveFrontendOptions) {
	fmt.Printf("reasonix %s — %s on http://%s\n", opts.command, ctrl.Label(), address)
	if srv.AuthMode() == "token" {
		fmt.Println("  auth: token")
		// Supervised Serve already owns the token file, so avoid logging its value.
		if opts.portFile != "" && opts.tokenFile != "" {
			fmt.Printf("  share: http://%s/ (token in %s)\n", address, opts.tokenFile)
		} else {
			fmt.Printf("  share: http://%s/#token=%s\n", address, url.QueryEscape(srv.AuthToken()))
		}
	} else if srv.AuthMode() == "password" {
		fmt.Printf("  auth: password (login at http://%s/login)\n", address)
	}
	if warning := serve.PlainHTTPAuthWarning(cfg, address); warning != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", warning)
	}
}

func startServeBalanceDiagnostics(ctrl *control.Controller) {
	go func() {
		if balance, err := ctrl.Balance(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "  balance: error — %v\n", err)
		} else if balance == nil {
			fmt.Fprintln(os.Stderr, "  balance: not configured (no balance_url for this provider)")
		} else {
			fmt.Printf("  balance: %s\n", balance.Display())
		}
	}()
}

func serveFrontendLoop(ctrl *control.Controller, srv *serve.Server, resources *serveFrontendResources, opts serveFrontendOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if resources.listener == nil {
		if err := srv.RunGraceful(ctx, opts.address); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		return 0
	}
	var serveErr error
	if opts.openBrowser {
		sessionID := ""
		if opts.hasSession {
			sessionID = agent.BranchID(ctrl.SessionPath())
		}
		serveErr = runServeListenerAfterReady(ctx, srv, resources.listener, resources.displayAddr, func() {
			browserURL, err := launchWebBrowser(srv, resources.displayAddr, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  browser: could not open %s — %v\n", browserURL, err)
			} else {
				fmt.Printf("  browser: %s\n", browserURL)
			}
		})
	} else {
		serveErr = srv.RunGracefulListener(ctx, resources.listener)
	}
	if serveErr != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, serveErr)
		return 1
	}
	return 0
}
