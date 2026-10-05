package update

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
)

// StudioCatalog is Studio's own rollback catalog, published by
// release-studio.yml. It is a constant rather than something a caller names: a
// catalog that could be pointed elsewhere is one whose entries would offer to
// "update" Studio into a different product.
const StudioCatalog = StudioMirror + "/studio/versions.json"

// Install is what a shell knows about itself that the kernel cannot work out.
// A Go process inside an Electron bundle resolves neither half: os.Executable()
// names the host binary, and the version is stamped into the application that
// spawned it. A shell that cannot answer leaves both empty and gets a hub that
// says what runs without offering to change it.
type Install struct {
	Version string
	Layout  Layout
}

// VersionEntry is one published release as a version panel shows it.
type VersionEntry struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes"`
	Current     bool   `json:"current"`
	Older       bool   `json:"older"`
}

// VersionHub is everything a version panel renders from. Err is carried beside
// the data rather than replacing it: an unreachable catalog must not hide which
// version is running.
type VersionHub struct {
	Current  string         `json:"current"`
	Pinned   string         `json:"pinned"`
	StalePin bool           `json:"stalePin"`
	Latest   string         `json:"latest"`
	Newer    bool           `json:"newer"`
	Versions []VersionEntry `json:"versions"`
	Err      string         `json:"err,omitempty"`
}

// catalogTimeout bounds the whole read. A version panel that hangs is worse
// than one that says the catalog could not be reached.
const catalogTimeout = 15 * time.Second

// Hub reads the rollback catalog for one install, so no shell holds a copy of
// what "newer", "pinned" or "latest" mean.
func Hub(ctx context.Context, in Install) VersionHub {
	client, err := netclient.NewHTTPClient(ProxySpec(), netclient.TransportOptions{})
	if err != nil {
		hub := VersionHub{Current: in.Version, Pinned: PinnedVersion()}
		hub.Err, hub.Versions = err.Error(), versionRows(nil, in.Version)
		return hub.nonNil()
	}
	return hubOver(ctx, in, client)
}

// hubOver is Hub with the route already decided, so a test can answer the
// catalog without reaching the network. The catalog URL stays a constant: what
// is injectable here is how the fetch travels, never where it lands.
func hubOver(ctx context.Context, in Install, client *http.Client) VersionHub {
	hub := VersionHub{Current: in.Version, Pinned: PinnedVersion()}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	st, err := New(Options{Current: in.Version, Pinned: hub.Pinned, HTTP: client, IndexURL: StudioCatalog, UserAgent: UserAgent(in.Version)}).Check(ctx)
	hub.Latest, hub.Newer, hub.StalePin = st.Latest, st.Newer, st.StalePin
	hub.Versions = versionRows(st.Entries, in.Version)
	if err != nil {
		hub.Err = err.Error()
	}
	return hub.nonNil()
}

// versionRows merges the running build into the catalog, newest first. The
// running row is always present even when the catalog cannot be read or does
// not carry it (a local build, a release still publishing): it is the panel's
// statement of what runs and its only handle for pinning, so it cannot depend
// on the network.
func versionRows(entries []IndexEntry, current string) []VersionEntry {
	rows := make([]VersionEntry, 0, len(entries)+1)
	seen := false
	for _, e := range entries {
		row := VersionEntry{Version: e.Version, Tag: e.Tag, PublishedAt: e.PublishedAt}
		if SameVersion(e.Version, current) {
			row.Current, seen = true, true
		} else {
			row.Older = e.IsOlderThan(current)
		}
		rows = append(rows, row)
	}
	if !seen && strings.TrimSpace(current) != "" {
		rows = append(rows, VersionEntry{Version: current, Current: true})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return CompareVersions(rows[i].Version, rows[j].Version) > 0
	})
	return rows
}

// nonNil keeps an empty catalog an empty list. A nil slice marshals to null,
// and a client that maps over it crashes its whole render.
func (h VersionHub) nonNil() VersionHub {
	if h.Versions == nil {
		h.Versions = []VersionEntry{}
	}
	return h
}

// PinnedVersion reads the release this machine is held on, or "" when it is
// free to follow the catalog.
func PinnedVersion() string {
	if cfg, err := config.Load(); err == nil && cfg != nil {
		return cfg.DesktopPinnedVersion()
	}
	return ""
}

// Pin holds this machine on a release, or releases the hold when version is
// empty. It is the half of a rollback that outlives the install: without it the
// updater would put the user back on the build they left.
func Pin(version string) error {
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetDesktopPinnedVersion(version); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

// ReleaseStalePin drops a pin that names a build other than the one running.
// Such a pin holds nobody anywhere: it is what a move that never landed, or an
// install from outside the app, leaves behind. A running build that is not a
// release (a source build says "dev") keeps the pin, since it proves nothing.
func ReleaseStalePin(running string) error {
	pinned := PinnedVersion()
	v := normalizeVersion(running)
	if pinned == "" || SameVersion(pinned, running) || v == "" || v[0] < '0' || v[0] > '9' {
		return nil
	}
	return Pin("")
}

// ProxySpec is the network route updates take, read from config so a machine
// behind a proxy reaches the catalog the same way everything else does.
func ProxySpec() netclient.ProxySpec {
	if cfg, err := config.Load(); err == nil && cfg != nil {
		return cfg.NetworkProxySpec()
	}
	return netclient.ProxySpec{Mode: netclient.ModeAuto}
}
