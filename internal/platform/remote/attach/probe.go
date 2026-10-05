package attach

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/platform/remote/bootstrap"
)

// Probe reads what a first connect to host would depend on. Like Browse it
// holds the connection and nothing above it — nothing is installed and no
// serve is started — so a machine this build has never reached can still say
// what it is missing, which is exactly when a reader needs to know.
func (p *Pool) Probe(ctx context.Context, host string) (bootstrap.Report, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return bootstrap.Report{}, errors.New("attach: no host named")
	}
	l, dialer := p.holdLink(host)
	if dialer {
		p.dial(l, Call{})
	}
	if err := l.wait(ctx); err != nil {
		p.dropLink(l)
		return bootstrap.Report{}, err
	}
	// Released on a timer, the way the picker's is: a probe is usually the
	// thing done just before connecting, and re-dialing in between would ask
	// for a passphrase twice.
	defer func() { time.AfterFunc(browseLinger, func() { p.dropLink(l) }) }()

	install := p.opts.Install
	if install == "" {
		install = l.install
	}
	// The same options serve would install under. A probe reporting a route
	// that attach would not take is worse than no probe.
	return bootstrap.Probe(ctx, l.client, bootstrap.Options{
		Install:        install,
		LocalBinary:    p.opts.LocalBinary,
		LocalGOOS:      runtime.GOOS,
		LocalGOARCH:    runtime.GOARCH,
		ProductVersion: p.opts.Version,
		FetchBinary:    p.opts.FetchBinary,
		MinVersion:     bootstrap.PaneFloor(p.brokers(l)),
	})
}
