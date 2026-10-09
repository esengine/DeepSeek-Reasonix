package market

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/fileutil"
)

// Why a listing came from disk instead of the registry.
const (
	CacheCauseUnreachable = "unreachable"
	CacheCauseBadResponse = "bad_response"
)

const (
	cacheEntryVersion = 1
	cacheMaxEntries   = 200
	cacheMaxBytes     = 8 << 20
	cacheMaxAge       = 7 * 24 * time.Hour
	cacheFreshFor     = time.Minute
)

// CacheNote marks an answer that is the last good copy of what the registry
// said, not what it says now.
type CacheNote struct {
	CachedAt string `json:"cachedAt"`
	Cause    string `json:"cause"`
}

// cacheNow is the clock the cache reads, replaced by tests.
var cacheNow = time.Now

var cacheFile = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// pruneMu serialises trimming across every cache in the process; readers and
// writers need no lock because each file is replaced atomically.
var pruneMu sync.Mutex

type cacheEntry struct {
	Version      int             `json:"v"`
	Key          string          `json:"key"`
	ETag         string          `json:"etag,omitempty"`
	LastModified string          `json:"lastModified,omitempty"`
	FetchedAt    time.Time       `json:"fetchedAt"`
	Body         json.RawMessage `json:"body"`
}

// diskCache keeps the last good answer per anonymous browse request. Only
// requests that carry no credential reach it, so nothing here is per-account.
type diskCache struct {
	dir        string
	maxEntries int
	maxBytes   int64
	maxAge     time.Duration
	freshFor   time.Duration
}

func newDiskCache(dir string) *diskCache {
	return &diskCache{dir: dir, maxEntries: cacheMaxEntries, maxBytes: cacheMaxBytes, maxAge: cacheMaxAge, freshFor: cacheFreshFor}
}

func (d *diskCache) fresh(e cacheEntry) bool {
	age := cacheNow().Sub(e.FetchedAt)
	return age >= 0 && age < d.freshFor
}

type refreshKey struct{}

// WithRefresh marks a read the person asked to repeat: it skips the fresh
// window of a stored copy and goes to the registry.
func WithRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, refreshKey{}, true)
}

func refreshAsked(ctx context.Context) bool { return ctx.Value(refreshKey{}) == true }

// noStore reports an answer the registry marked private or not to be kept,
// which is how it labels anything tied to an identity or not a plain success.
func noStore(h http.Header) bool {
	for _, line := range h.Values("Cache-Control") {
		for part := range strings.SplitSeq(line, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if n := strings.ToLower(name); n == "no-store" || n == "private" {
				return true
			}
		}
	}
	return false
}

func (d *diskCache) key(host, path string, query url.Values) string {
	sum := sha256.Sum256([]byte("market-cache/v1\n" + host + "\n" + path + "\n" + query.Encode()))
	return hex.EncodeToString(sum[:])
}

func (d *diskCache) path(key string) string { return filepath.Join(d.dir, key+".json") }

func (d *diskCache) load(key string) (cacheEntry, bool) {
	f, err := os.Open(d.path(key))
	if err != nil {
		return cacheEntry{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, d.maxBytes+1))
	if err != nil || int64(len(raw)) > d.maxBytes {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if json.Unmarshal(raw, &e) != nil || e.Version != cacheEntryVersion || e.Key != key || !json.Valid(e.Body) {
		return cacheEntry{}, false
	}
	if age := cacheNow().Sub(e.FetchedAt); age > d.maxAge || age < -time.Hour {
		return cacheEntry{}, false
	}
	return e, true
}

func (d *diskCache) store(key string, h http.Header, body []byte) {
	e := cacheEntry{Version: cacheEntryVersion, Key: key, ETag: h.Get("ETag"), LastModified: h.Get("Last-Modified"), FetchedAt: cacheNow().UTC(), Body: body}
	d.write(e)
}

func (d *diskCache) write(e cacheEntry) {
	raw, err := json.Marshal(e)
	if err != nil || int64(len(raw)) > d.maxBytes {
		return
	}
	if os.MkdirAll(d.dir, 0o700) != nil {
		return
	}
	if fileutil.AtomicWriteFile(d.path(e.Key), raw, 0o600) != nil {
		return
	}
	d.prune()
}

func (d *diskCache) remove(key string) { _ = os.Remove(d.path(key)) }

func (d *diskCache) prune() {
	pruneMu.Lock()
	defer pruneMu.Unlock()
	items, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}
	type held struct {
		name string
		at   time.Time
		size int64
	}
	var kept []held
	var total int64
	for _, it := range items {
		if it.IsDir() || !cacheFile.MatchString(it.Name()) {
			continue
		}
		info, err := it.Info()
		if err != nil {
			continue
		}
		if cacheNow().Sub(info.ModTime()) > d.maxAge {
			_ = os.Remove(filepath.Join(d.dir, it.Name()))
			continue
		}
		kept = append(kept, held{it.Name(), info.ModTime(), info.Size()})
		total += info.Size()
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.Before(kept[j].at) })
	for len(kept) > 0 && (len(kept) > d.maxEntries || total > d.maxBytes) {
		_ = os.Remove(filepath.Join(d.dir, kept[0].name))
		total -= kept[0].size
		kept = kept[1:]
	}
}
