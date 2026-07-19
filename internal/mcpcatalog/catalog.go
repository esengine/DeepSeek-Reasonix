// Package mcpcatalog verifies and caches the signed Reasonix MCP plugin catalog.
// Catalog data is host-local policy and never enters provider-visible prompts or
// tool definitions.
package mcpcatalog

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"aead.dev/minisign"

	"reasonix/internal/fileutil"
)

const (
	SchemaVersion  = 1
	CatalogURL     = "https://dl.reasonix.io/plugins/catalog/v1/index.json"
	SignatureURL   = CatalogURL + ".minisig"
	maxCatalogSize = 4 << 20
	staleAfter     = 30 * 24 * time.Hour
)

// Dedicated catalog key. This is intentionally distinct from the desktop
// release-signing key. Additional keys may be appended during an app release
// for rotation; remote catalog data can never add a trust root.
var PublicKeys = []string{
	`untrusted comment: minisign public key: 1AB246842CC91DE1
RWThHckshEayGlxrbXAAaeyiQeXLwmMPSzVyh11HwJ1VCKXWMWCyJCqo`,
}

var validEntryID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,191}$`)
var validCommit = regexp.MustCompile(`^[A-Fa-f0-9]{40}([A-Fa-f0-9]{24})?$`)

var runtimeCatalog struct {
	sync.RWMutex
	revoked map[string]bool
}

//go:embed catalog-v1.json catalog-v1.json.minisig
var bundledFS embed.FS

type Index struct {
	SchemaVersion int          `json:"schema_version"`
	Sequence      uint64       `json:"sequence"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Entries       []Entry      `json:"entries"`
	Revocations   []Revocation `json:"revocations,omitempty"`
}

type Entry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Source         string   `json:"source"`
	Commit         string   `json:"commit"`
	PackageSHA256  string   `json:"package_sha256"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	Servers        []Server `json:"mcp_servers,omitempty"`
}

type Server struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Network   bool     `json:"network"`
	Readers   []string `json:"readers,omitempty"`
}

type Revocation struct {
	EntryID string `json:"entry_id"`
	Reason  string `json:"reason,omitempty"`
}

type Source string

const (
	SourceRemote  Source = "remote"
	SourceCached  Source = "cached"
	SourceBundled Source = "bundled"
)

type Result struct {
	Index   Index
	Source  Source
	Offline bool
	Stale   bool
}

type Loader struct {
	CacheDir     string
	Client       *http.Client
	CatalogURL   string
	SignatureURL string
	Keys         []string
}

func CachePaths(cacheDir string) (string, string) {
	return filepath.Join(cacheDir, "mcp-catalog-v1.json"), filepath.Join(cacheDir, "mcp-catalog-v1.json.minisig")
}

func cacheEnvelopePath(cacheDir string) string {
	return filepath.Join(cacheDir, "mcp-catalog-v1.lkg.json")
}

type cacheEnvelope struct {
	Data      []byte `json:"data"`
	Signature []byte `json:"signature"`
}

func (l Loader) Load(ctx context.Context, refresh bool) (Result, error) {
	if refresh {
		if result, err := l.Refresh(ctx); err == nil {
			rememberRuntimeIndex(result.Index)
			return result, nil
		}
	}
	result, err := l.loadNewestLocal()
	if err != nil {
		return Result{}, err
	}
	result.Offline = refresh
	result.Stale = catalogStale(result.Index, time.Now())
	rememberRuntimeIndex(result.Index)
	return result, nil
}

func (l Loader) Refresh(ctx context.Context) (Result, error) {
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	catalogURL := strings.TrimSpace(l.CatalogURL)
	if catalogURL == "" {
		catalogURL = CatalogURL
	}
	sigURL := strings.TrimSpace(l.SignatureURL)
	if sigURL == "" {
		sigURL = SignatureURL
	}
	data, err := fetch(ctx, client, catalogURL)
	if err != nil {
		return Result{}, err
	}
	sig, err := fetch(ctx, client, sigURL)
	if err != nil {
		return Result{}, err
	}
	idx, err := Verify(data, sig, l.keys())
	if err != nil {
		return Result{}, err
	}
	if local, localErr := l.loadNewestLocal(); localErr == nil && idx.Sequence < local.Index.Sequence {
		return Result{}, fmt.Errorf("MCP catalog sequence rollback: remote=%d local=%d source=%s", idx.Sequence, local.Index.Sequence, local.Source)
	}
	if strings.TrimSpace(l.CacheDir) != "" {
		body, marshalErr := json.Marshal(cacheEnvelope{Data: data, Signature: sig})
		if marshalErr != nil {
			return Result{}, marshalErr
		}
		// Store the verified pair in one atomic envelope. Two independent file
		// renames can crash between JSON and signature and destroy the LKG.
		if err := fileutil.AtomicWriteFile(cacheEnvelopePath(l.CacheDir), body, 0o600); err != nil {
			return Result{}, err
		}
	}
	rememberRuntimeIndex(idx)
	return Result{Index: idx, Source: SourceRemote, Stale: catalogStale(idx, time.Now())}, nil
}

func (l Loader) loadNewestLocal() (Result, error) {
	cached, cachedErr := l.loadCached()
	bundled, bundledErr := loadBundled(l.keys())
	switch {
	case cachedErr == nil && bundledErr == nil:
		return newestCatalogResult(cached, bundled), nil
	case cachedErr == nil:
		return cached, nil
	case bundledErr == nil:
		return bundled, nil
	default:
		return Result{}, fmt.Errorf("load local MCP catalog: cached: %v; bundled: %v", cachedErr, bundledErr)
	}
}

func newestCatalogResult(a, b Result) Result {
	if b.Index.Sequence >= a.Index.Sequence {
		return b
	}
	return a
}

func catalogStale(index Index, now time.Time) bool {
	return index.GeneratedAt.IsZero() || now.Sub(index.GeneratedAt) > staleAfter
}

func rememberRuntimeIndex(index Index) {
	revoked := make(map[string]bool, len(index.Revocations))
	for _, revocation := range index.Revocations {
		if entryID := strings.TrimSpace(revocation.EntryID); entryID != "" {
			revoked[entryID] = true
		}
	}
	runtimeCatalog.Lock()
	runtimeCatalog.revoked = revoked
	runtimeCatalog.Unlock()
}

// RuntimeEntryRevoked lets a lazy MCP startup observe a catalog revocation
// fetched after its Spec was composed but before its process connects.
func RuntimeEntryRevoked(entryID string) bool {
	runtimeCatalog.RLock()
	defer runtimeCatalog.RUnlock()
	return runtimeCatalog.revoked[strings.TrimSpace(entryID)]
}

func (idx Index) RevokedEntryIDs() map[string]bool {
	out := make(map[string]bool, len(idx.Revocations))
	for _, revocation := range idx.Revocations {
		if entryID := strings.TrimSpace(revocation.EntryID); entryID != "" {
			out[entryID] = true
		}
	}
	return out
}

func (l Loader) loadCached() (Result, error) {
	if strings.TrimSpace(l.CacheDir) == "" {
		return Result{}, os.ErrNotExist
	}
	body, envelopeErr := os.ReadFile(cacheEnvelopePath(l.CacheDir))
	var data, sig []byte
	if envelopeErr == nil {
		var envelope cacheEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			return Result{}, fmt.Errorf("parse cached MCP catalog envelope: %w", err)
		}
		data, sig = envelope.Data, envelope.Signature
	} else {
		// Two-file caches are accepted only as a migration fallback from early
		// v1 builds. Every successful refresh rewrites the atomic envelope.
		dataPath, sigPath := CachePaths(l.CacheDir)
		var err error
		data, err = os.ReadFile(dataPath)
		if err != nil {
			return Result{}, envelopeErr
		}
		sig, err = os.ReadFile(sigPath)
		if err != nil {
			return Result{}, err
		}
	}
	idx, err := Verify(data, sig, l.keys())
	if err != nil {
		return Result{}, err
	}
	return Result{Index: idx, Source: SourceCached}, nil
}

func (l Loader) keys() []string {
	if len(l.Keys) > 0 {
		return l.Keys
	}
	return PublicKeys
}

func loadBundled(keys []string) (Result, error) {
	data, err := bundledFS.ReadFile("catalog-v1.json")
	if err != nil {
		return Result{}, err
	}
	sig, err := bundledFS.ReadFile("catalog-v1.json.minisig")
	if err != nil {
		return Result{}, err
	}
	idx, err := Verify(data, sig, keys)
	if err != nil {
		return Result{}, fmt.Errorf("verify bundled MCP catalog: %w", err)
	}
	return Result{Index: idx, Source: SourceBundled}, nil
}

func Verify(data, sig []byte, keys []string) (Index, error) {
	if len(data) == 0 || len(data) > maxCatalogSize {
		return Index{}, fmt.Errorf("invalid MCP catalog size %d", len(data))
	}
	verified := false
	var parseErrors []string
	for _, text := range keys {
		var key minisign.PublicKey
		if err := key.UnmarshalText([]byte(text)); err != nil {
			parseErrors = append(parseErrors, err.Error())
			continue
		}
		if minisign.Verify(key, data, sig) {
			verified = true
			break
		}
	}
	if !verified {
		if len(parseErrors) == len(keys) && len(keys) > 0 {
			return Index{}, fmt.Errorf("parse MCP catalog public keys: %s", strings.Join(parseErrors, "; "))
		}
		return Index{}, errors.New("MCP catalog signature verification failed")
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, fmt.Errorf("parse MCP catalog: %w", err)
	}
	if err := Validate(idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

func Validate(idx Index) error {
	if idx.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported MCP catalog schema version %d", idx.SchemaVersion)
	}
	seenIDs, seenVersions := map[string]bool{}, map[string]bool{}
	for _, entry := range idx.Entries {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Version) == "" {
			return errors.New("MCP catalog entry requires id, name, and version")
		}
		if strings.TrimSpace(entry.Source) == "" || !validCommit.MatchString(strings.TrimSpace(entry.Commit)) {
			return fmt.Errorf("MCP catalog entry %q requires a source and exact 40- or 64-hex commit", entry.ID)
		}
		if !validEntryID.MatchString(entry.ID) {
			return fmt.Errorf("MCP catalog entry has invalid id %q", entry.ID)
		}
		if seenIDs[entry.ID] {
			return fmt.Errorf("duplicate MCP catalog entry id %q", entry.ID)
		}
		seenIDs[entry.ID] = true
		versionKey := entry.Name + "\x00" + entry.Version
		if seenVersions[versionKey] {
			return fmt.Errorf("duplicate MCP catalog plugin version %s@%s", entry.Name, entry.Version)
		}
		seenVersions[versionKey] = true
		if !validSHA256(entry.PackageSHA256) || !validSHA256(entry.ManifestSHA256) {
			return fmt.Errorf("MCP catalog entry %q has invalid package or manifest digest", entry.ID)
		}
		serverNames := map[string]bool{}
		for _, server := range entry.Servers {
			if strings.TrimSpace(server.Name) == "" {
				return fmt.Errorf("MCP catalog entry %q has unnamed server", entry.ID)
			}
			switch strings.ToLower(strings.TrimSpace(server.Transport)) {
			case "stdio", "http", "streamable-http", "sse":
			default:
				return fmt.Errorf("MCP catalog entry %q server %q has unsupported transport %q", entry.ID, server.Name, server.Transport)
			}
			if serverNames[server.Name] {
				return fmt.Errorf("MCP catalog entry %q has duplicate server %q", entry.ID, server.Name)
			}
			serverNames[server.Name] = true
			readers := append([]string(nil), server.Readers...)
			sort.Strings(readers)
			for i, reader := range readers {
				if strings.TrimSpace(reader) == "" {
					return fmt.Errorf("MCP catalog entry %q server %q has an empty reader name", entry.ID, server.Name)
				}
				if strings.TrimSpace(reader) == "" || (i > 0 && reader == readers[i-1]) {
					return fmt.Errorf("MCP catalog entry %q server %q has invalid reader declarations", entry.ID, server.Name)
				}
			}
		}
	}
	for _, rev := range idx.Revocations {
		if strings.TrimSpace(rev.EntryID) == "" {
			return errors.New("MCP catalog revocation requires entry_id")
		}
	}
	seenRevocations := map[string]bool{}
	for _, revocation := range idx.Revocations {
		entryID := strings.TrimSpace(revocation.EntryID)
		if entryID == "" || seenRevocations[entryID] {
			return fmt.Errorf("MCP catalog has invalid or duplicate revocation %q", entryID)
		}
		seenRevocations[entryID] = true
	}
	return nil
}

// Parse validates unsigned catalog source before it is signed for publication.
// Runtime callers must use Verify so untrusted remote bytes cannot bypass the
// catalog signature.
func Parse(data []byte) (Index, error) {
	if len(data) == 0 || len(data) > maxCatalogSize {
		return Index{}, fmt.Errorf("invalid MCP catalog size %d", len(data))
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var idx Index
	if err := dec.Decode(&idx); err != nil {
		return Index{}, fmt.Errorf("parse MCP catalog: %w", err)
	}
	if err := Validate(idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

func (idx Index) Match(name, version, source, commit, packageDigest string) (Entry, bool) {
	for _, entry := range idx.Entries {
		if entry.Name != name || entry.Version != version || entry.Source != source || !strings.EqualFold(entry.Commit, commit) || !strings.EqualFold(entry.PackageSHA256, packageDigest) {
			continue
		}
		if idx.IsRevoked(entry.ID) {
			return Entry{}, false
		}
		return entry, true
	}
	return Entry{}, false
}

func (idx Index) IsRevoked(entryID string) bool {
	for _, rev := range idx.Revocations {
		if rev.EntryID == entryID {
			return true
		}
	}
	return false
}

func TreeSHA256(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	type item struct {
		path string
		body []byte
	}
	var items []item
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("MCP catalog package contains symlink %q", filepath.ToSlash(rel))
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("MCP catalog package contains non-regular file %q", filepath.ToSlash(rel))
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		items = append(items, item{path: filepath.ToSlash(rel), body: body})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	h := sha256.New()
	for _, item := range items {
		// Permission bits are intentionally excluded: Windows checkouts do not
		// preserve POSIX modes, so including them makes a catalog package verify
		// on Linux but silently lose official status on Windows. Paths, lengths,
		// and bytes still bind the complete regular-file tree.
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", item.path, len(item.body))
		_, _ = h.Write(item.body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch MCP catalog %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCatalogSize {
		return nil, fmt.Errorf("MCP catalog response exceeds %d bytes", maxCatalogSize)
	}
	return body, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
