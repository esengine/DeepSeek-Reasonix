// Package remotehost dials the machines a Studio can open a workspace on. It
// is the standard implementation of serve's RemoteAttacher: everything about
// holding a connection, installing a kernel over there and naming what went
// wrong. None of it is a window's business, so neither shell owns it.
package remotehost

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/model/providerbroker"
	"reasonix/internal/platform/releaseasset"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/attach"
	"reasonix/internal/platform/remote/bootstrap"
)

// Link gives the hub what it needs for panes on other machines: a pool that
// dials, and the prompts a first connect may have to stop for.
type Link struct {
	pool   *attach.Pool
	broker *providerbroker.Local
}

// New builds the link layer for a host. asks is where a first-seen key or a
// locked one goes; nil leaves both prompts unset, and the strict path applies —
// a key nobody looked at is refused rather than accepted quietly.
func New(ctx context.Context, version string, asks *serve.AskBroker) *Link {
	// This window's own providers, on loopback for the kernels it starts
	// elsewhere. One that will not listen is no reason to refuse remote work:
	// those hosts resolve their own, as every one of them used to.
	broker, err := providerbroker.Listen(boot.LiveProviderResolver{})
	if err != nil {
		slog.Warn("remotehost: the provider broker did not start; remote hosts will need their own credentials", "err", err)
		broker = nil
	}
	return &Link{broker: broker, pool: attach.NewPool(ctx, attach.Options{
		Prompts: prompts{asks: asks}.build(),
		Version: version,
		Broker:  brokerFor(broker),
		// No LocalBinary: a shell's executable is the window, not the CLI, and
		// uploading it would spend the transfer to have the far side reject a
		// binary with no serve command. npm, then a verified release download.
		FetchBinary:     fetchRemoteBinary,
		ResolveDownload: resolveRemoteDownload,
	})}
}

func brokerFor(l *providerbroker.Local) attach.Broker {
	if l == nil {
		return attach.Broker{}
	}
	return attach.Broker{Addr: l.Addr, Token: l.Token}
}

// Close stops the broker this link published. The pool's connections belong to
// the context it was built with and end with it.
func (r *Link) Close() error {
	if r == nil {
		return nil
	}
	return r.broker.Close()
}

func (r *Link) Attach(ctx context.Context, host, workspace string) (serve.RemoteEndpoint, func(), error) {
	ep, err := r.pool.Attach(ctx, host, workspace, attach.Call{})
	if err != nil {
		return serve.RemoteEndpoint{}, nil, identify(host, err)
	}
	return serve.RemoteEndpoint{
		Host:      ep.Host,
		Workspace: ep.Workspace,
		Addr:      ep.Addr,
		Token:     ep.Token,
	}, ep.Release, nil
}

// Browse lists folders over the connection alone. The pool holds the link for
// a moment afterwards, so walking down a tree is one login rather than one per
// step — and nothing is installed on the far side to answer it, which is what
// lets a folder be chosen before that machine has ever run a kernel.
func (r *Link) Browse(ctx context.Context, host, dir string) (serve.RemoteListing, error) {
	listing, err := r.pool.Browse(ctx, host, dir)
	if err != nil {
		return serve.RemoteListing{}, identifyBrowse(host, dir, err)
	}
	out := serve.RemoteListing{Path: listing.Path, Parent: listing.Parent, Truncated: listing.Truncated}
	out.Folders = make([]serve.RemoteFolder, 0, len(listing.Folders))
	for _, f := range listing.Folders {
		out.Folders = append(out.Folders, serve.RemoteFolder{Name: f.Name, Path: f.Path})
	}
	return out, nil
}

// identifyBrowse separates what the reader fixes by typing a different path
// from what they fix by looking at the connection. Only this side sees the file
// protocol's answer, and a mistyped folder arriving as a bad gateway sends
// someone to check a link that is up.
func identifyBrowse(host, dir string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return serve.Refusal(http.StatusNotFound, "remote.no_such_folder", err,
			map[string]any{"host": host, "path": dir})
	case errors.Is(err, fs.ErrPermission):
		return serve.Refusal(http.StatusForbidden, "remote.folder_unreadable", err,
			map[string]any{"host": host, "path": dir})
	}
	return identify(host, err)
}

// Candidates reads the machine's own ssh_config. Filling the book by hand when
// the addresses are already written down next door is the step people skip.
func (r *Link) Candidates() []string {
	src, err := remote.LoadUserSSHConfig()
	if err != nil || src == nil {
		return nil
	}
	out := make([]string, 0)
	for _, cand := range src.Aliases() {
		out = append(out, cand.Alias)
	}
	return out
}

// Probe answers what that machine can do before anything is attempted on it.
// A closed route carries the code identify would have refused with, so the
// window has one wording for a failure and for the same failure foreseen.
func (r *Link) Probe(ctx context.Context, host string) (serve.RemoteProbe, error) {
	rep, err := r.pool.Probe(ctx, host)
	if err != nil {
		return serve.RemoteProbe{}, identify(host, err)
	}
	out := serve.RemoteProbe{
		OS: rep.OS, Arch: rep.Arch, Home: rep.Home,
		Kernel: rep.Kernel, Version: rep.Version, Outdated: rep.Outdated,
		NPM: rep.NPM, Ready: rep.Ready(),
	}
	for _, route := range rep.Routes {
		out.Routes = append(out.Routes, serve.RemoteProbeRoute{
			Name: route.Name, OK: route.OK(), Code: installFailureCode(route.Err),
		})
	}
	return out, nil
}

func (r *Link) States() map[string]serve.RemoteLinkState {
	live := r.pool.States()
	out := make(map[string]serve.RemoteLinkState, len(live))
	for host, st := range live {
		out[host] = serve.RemoteLinkState{
			Status:  st.Status.String(),
			Attempt: st.Attempt,
			Step:    st.Step,
			Detail:  st.Detail,
			Err:     st.Err,
			Panes:   st.Panes,
		}
	}
	return out
}

// fetchRemoteBinary downloads the release for a remote's platform when the host
// has no reasonix and cannot install one itself.
func fetchRemoteBinary(ctx context.Context, version, goos, goarch string) ([]byte, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{
		ResponseHeaderTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	client.Timeout = 2 * time.Minute
	return releaseasset.DownloadCLI(ctx, client, config.CacheDir(), releaseasset.StudioLine, version, goos, goarch)
}

// resolveRemoteDownload names the release archive and its digest so the far
// machine can pull it over its own connection. Only SHA256SUMS is read here.
func resolveRemoteDownload(ctx context.Context, version, goos, goarch string) (releaseasset.CLIDownload, error) {
	cfg, err := config.Load()
	if err != nil {
		return releaseasset.CLIDownload{}, err
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{
		ResponseHeaderTimeout: 30 * time.Second,
	})
	if err != nil {
		return releaseasset.CLIDownload{}, err
	}
	client.Timeout = 30 * time.Second
	return releaseasset.ResolveCLIDownload(ctx, client, releaseasset.StudioLine, version, goos, goarch)
}

// installFailureCode names which way putting a kernel over there failed. One
// table for two readers: identify refuses with it, and a probe reports it for
// a route already visibly closed, so one wording covers both. Order matters —
// the auto strategy joins every attempt, so specific routes are matched before
// the "nothing left" that wraps them.
func installFailureCode(err error) string {
	switch {
	case errors.Is(err, bootstrap.ErrInstallDisabled):
		return "remote.install_disabled"
	// Ahead of the per-route codes: when this one is in the join it is why the
	// download routes closed, and naming npm's symptom instead sends the reader
	// to a machine that is fine.
	case errors.Is(err, bootstrap.ErrNoReleaseForBuild):
		return "remote.no_release_for_build"
	case errors.Is(err, bootstrap.ErrNPMOutsidePath):
		return "remote.npm_outside_path"
	case errors.Is(err, bootstrap.ErrBinaryNotRunnable):
		return "remote.binary_not_runnable"
	case errors.Is(err, bootstrap.ErrServeDidNotStart):
		return "remote.serve_did_not_start"
	case errors.Is(err, bootstrap.ErrNoInstallPath):
		return "remote.no_install_path"
	case errors.Is(err, bootstrap.ErrNPMUnavailable):
		return "remote.npm_unavailable"
	case errors.Is(err, bootstrap.ErrPlatformMismatch):
		return "remote.platform_mismatch"
	}
	return ""
}

// identify gives the failures a person can act on an identity of their own. A
// changed host key is the one that must never arrive as a network error: the
// record it contradicts is what makes it checkable, and there is deliberately
// no path from here to connecting anyway.
func identify(host string, err error) error {
	var mismatch *remote.HostKeyMismatchError
	if errors.As(err, &mismatch) {
		params := map[string]any{"host": mismatch.Host, "fingerprint": mismatch.PresentedFingerprint}
		if len(mismatch.Locations) > 0 {
			params["file"] = mismatch.Locations[0].Filename
			params["line"] = mismatch.Locations[0].Line
		}
		return serve.Refusal(http.StatusConflict, "remote.host_key_changed", err, params)
	}
	var tooOld *bootstrap.KernelTooOldError
	switch {
	case errors.As(err, &tooOld):
		// The machine has a reasonix; it is from a line with no pane hub, so it
		// would answer everything a pane asked with 405. Caught here rather
		// than there: this is the side that read its version.
		return serve.Refusal(http.StatusBadGateway, "remote.kernel_too_old", err, map[string]any{"host": host})
	case errors.Is(err, remote.ErrHostKeyRejected):
		return serve.Refusal(http.StatusForbidden, "remote.host_key_rejected", err, nil)
	case errors.Is(err, remote.ErrAuthFailed):
		return serve.Refusal(http.StatusUnauthorized, "remote.auth_failed", err, nil)
	case errors.Is(err, bootstrap.ErrUnsupportedRemote):
		// The machine answered; it just is not one a kernel can be installed
		// onto. No detail: what it said is its own shell's complaint, in its
		// own code page, and pasting that on screen explains nothing.
		return serve.Refusal(http.StatusNotImplemented, "remote.unsupported_os", err, map[string]any{"host": host})
	}
	if code := installFailureCode(err); code != "" {
		status := http.StatusBadGateway
		if code == "remote.install_disabled" {
			// Nothing is broken; this machine is set not to install one.
			status = http.StatusConflict
		}
		return serve.Refusal(status, code, err, map[string]any{"host": host})
	}
	// Everything else keeps its text, which without a code reaches the window
	// as a bare status — and a bare 502 reads as "the request never arrived"
	// when what actually happened is on the other end of the link.
	return serve.Refusal(http.StatusBadGateway, "remote.attach_failed", err,
		map[string]any{"host": host, "detail": err.Error()})
}

// prompts turns the link layer's blocking callbacks into canonical
// questions. Nothing here holds one: the broker does, and every shell finds it
// the same way — a window that is not polling yet simply has not looked.
type prompts struct{ asks *serve.AskBroker }

// prompts are what the pool hands the link layer. A broker that does not exist
// leaves both nil, and the strict path applies: a first-seen key is refused
// rather than accepted quietly.
func (p prompts) build() attach.Prompts {
	if p.asks == nil {
		return attach.Prompts{}
	}
	return attach.Prompts{Secret: p.secret, HostKey: p.hostKey}
}

func (p prompts) hostKey(ctx context.Context, q remote.HostKeyQuestion) (bool, error) {
	answer, err := p.asks.Ask(ctx, serve.Ask{
		Kind:        "hostkey",
		Host:        q.Host,
		Address:     q.Address,
		KeyType:     q.KeyType,
		Fingerprint: q.Fingerprint,
	})
	if err != nil {
		return false, err
	}
	return answer.OK, nil
}

func (p prompts) secret(ctx context.Context, kind remote.SecretKind, host, identityFile string) (string, error) {
	answer, err := p.asks.Ask(ctx, serve.Ask{
		Kind:         kind.String(),
		Host:         host,
		IdentityFile: identityFile,
	})
	if err != nil {
		return "", err
	}
	if !answer.OK {
		return "", errors.New("remote: cancelled")
	}
	return answer.Text, nil
}
