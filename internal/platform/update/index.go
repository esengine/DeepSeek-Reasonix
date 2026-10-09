package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"reasonix/internal/contract/provider"
)

// Index is versions.json. It deliberately carries no asset URLs or signatures —
// an entry points at that version's own manifest, so rolling back resolves and
// verifies through the same path a forward update does.
type Index struct {
	SchemaVersion int          `json:"schemaVersion"`
	UpdatedAt     string       `json:"updatedAt"`
	Versions      []IndexEntry `json:"versions"`
}

// IndexEntry is one published release, newest first in Index.Versions.
type IndexEntry struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Channel     string `json:"channel"`
	PublishedAt string `json:"publishedAt"`
	Manifest    string `json:"manifest"`
	// Notes is set once the release's notes are published. Its value is never
	// fetched: it says only that they exist (see NotesURL).
	Notes string `json:"notes,omitempty"`
}

// FetchIndex reads the rollback catalog; userAgent "" is the client identity.
// A newer schemaVersion and unknown fields are tolerated so an old build can
// read it. Entries without a version or manifest are dropped rather than
// failing the list: one malformed row must not cost every other version.
func FetchIndex(ctx context.Context, c *http.Client, url, userAgent string) (*Index, error) {
	// No default: the catalog is per line, and falling back to another line's
	// URL is how a caller that forgot to name one gets a silent 404.
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("update: no catalog URL")
	}
	body, err := fetchJSON(ctx, c, url, userAgent)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("update: parse %s: %w", url, err)
	}
	kept := idx.Versions[:0]
	for _, e := range idx.Versions {
		if strings.TrimSpace(e.Version) != "" && strings.TrimSpace(e.Manifest) != "" {
			kept = append(kept, e)
		}
	}
	idx.Versions = kept
	return &idx, nil
}

// FetchManifestAt reads one version's immutable manifest.
func FetchManifestAt(ctx context.Context, c *http.Client, url, userAgent string) (*Manifest, error) {
	body, err := fetchJSON(ctx, c, url, userAgent)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("update: parse %s: %w", url, err)
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, fmt.Errorf("update: %s carries no version", url)
	}
	return &m, nil
}

// Rollbackable reports the entries a user may move to from current, newest
// first. The running version is excluded — "go back to where you already are"
// is not an offer — and so is anything this build cannot install.
func (i *Index) Rollbackable(current string) []IndexEntry {
	if i == nil {
		return nil
	}
	var out []IndexEntry
	for _, e := range i.Versions {
		if SameVersion(e.Version, current) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// SameVersion compares release versions tolerating the leading v the tag and
// the manifest disagree about.
func SameVersion(a, b string) bool {
	return strings.EqualFold(normalizeVersion(a), normalizeVersion(b))
}

// CompareVersions orders two release versions: -1 when a is older. The core is
// compared numerically (1.9.0 is older than 1.25.0, which a lexical compare gets
// backwards), and a release outranks its own prereleases — otherwise moving from
// 2.0.0-rc1 to 2.0.0 would be presented as a rollback.
func CompareVersions(a, b string) int {
	aCore, aPre := splitPrerelease(normalizeVersion(a))
	bCore, bPre := splitPrerelease(normalizeVersion(b))
	as, bs := strings.Split(aCore, "."), strings.Split(bCore, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := segmentAt(as, i), segmentAt(bs, i)
		xn, xErr := strconv.Atoi(x)
		yn, yErr := strconv.Atoi(y)
		if xErr != nil || yErr != nil {
			if x != y {
				return sign(strings.Compare(x, y))
			}
			continue
		}
		if xn != yn {
			return sign(xn - yn)
		}
	}
	switch {
	case aPre == bPre:
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	default:
		return sign(strings.Compare(aPre, bPre))
	}
}

// splitPrerelease separates 2.0.0-rc1 into its core and its prerelease tag.
func splitPrerelease(v string) (core, pre string) {
	if core, pre, ok := strings.Cut(v, "-"); ok {
		return core, pre
	}
	return v, ""
}

// IsOlderThan reports whether the entry is behind current — the direction that
// makes a move a rollback rather than an update.
func (e IndexEntry) IsOlderThan(current string) bool {
	return CompareVersions(e.Version, current) < 0
}

func segmentAt(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return "0"
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// fetchJSON names the caller as userAgent, or as the build's client identity
// when none is given: edge bot protection answers Go's default with 403.
func fetchJSON(ctx context.Context, c *http.Client, url, userAgent string) ([]byte, error) {
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		provider.ApplyClientIdentity(req)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
