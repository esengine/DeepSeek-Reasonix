package bootstrap

import (
	"context"
	"fmt"
	"strings"
)

// Environment the launched serve reads to reach the desktop browser broker.
// Both travel in the process environment only: the token rotates with every
// SSH connection generation, so nothing on disk may outlive it.
const (
	BrowserBrokerEnv = "REASONIX_BROWSER_BROKER"
	BrowserTokenEnv  = "REASONIX_BROWSER_TOKEN"
)

// ServeBrowserBrokerMarker is the `serve --help` flag name that advertises a
// binary able to use a desktop browser broker. Unlike the required capability
// markers it is optional: a serve without it launches untouched.
const ServeBrowserBrokerMarker = "browser-broker"

// BrowserBrokerOptions hands a launching serve the desktop browser broker:
// the reverse-forwarded loopback endpoint on the REMOTE host and the bearer
// token of the current connection generation.
type BrowserBrokerOptions struct {
	BaseURL string
	Token   string
}

// BrowserBrokerSupportedCommand prints "yes" when bin's serve command
// advertises ServeBrowserBrokerMarker, "no" otherwise.
func BrowserBrokerSupportedCommand(bin string) string {
	return fmt.Sprintf(
		"if %s serve --help 2>&1 | grep -q -- %s; then echo yes; else echo no; fi",
		shellQuote(bin), shellQuote(ServeBrowserBrokerMarker),
	)
}

// browserEnvPrefix renders the environment assignments placed before the
// serve command; empty when no broker is configured.
func browserEnvPrefix(opts *BrowserBrokerOptions) string {
	if opts == nil || strings.TrimSpace(opts.BaseURL) == "" || strings.TrimSpace(opts.Token) == "" {
		return ""
	}
	return BrowserBrokerEnv + "=" + shellQuote(strings.TrimSpace(opts.BaseURL)) + " " +
		BrowserTokenEnv + "=" + shellQuote(strings.TrimSpace(opts.Token)) + " "
}

// resolveBrowserBroker asks the desktop for broker options right before a
// fresh launch, and only when bin advertises the capability, so the reverse
// forward is never installed for a serve that cannot use it. A broker that
// cannot be prepared degrades to a launch without browser access.
func resolveBrowserBroker(ctx context.Context, conn Conn, bin string, opts Options) *BrowserBrokerOptions {
	if opts.BrowserBroker == nil {
		return nil
	}
	res, err := conn.Exec(ctx, BrowserBrokerSupportedCommand(bin))
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "yes" {
		return nil
	}
	broker, err := opts.BrowserBroker(ctx)
	if err != nil {
		opts.progress("browser_broker", "unavailable: "+err.Error())
		return nil
	}
	if browserEnvPrefix(broker) == "" {
		return nil
	}
	return broker
}
