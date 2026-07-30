// Package mcpregistry provides the explicit browse/install path for the
// official Model Context Protocol Registry. It is never called during boot or
// tool discovery, so a slow or unavailable registry cannot affect a session.
package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

const DefaultBaseURL = "https://registry.modelcontextprotocol.io"

const (
	defaultLimit = 20
	maxLimit     = 100
	maxBody      = 8 << 20
	maxCacheAge  = 30 * 24 * time.Hour
)

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	CachePath string
	Now       func() time.Time
}

func New(cachePath string) *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		CachePath: cachePath,
		Now:       time.Now,
	}
}

// Entry is one registry server reduced to the configuration Reasonix can
// install without prompting for missing secrets or server-specific arguments.
type Entry struct {
	Name              string   `json:"name"`
	SuggestedName     string   `json:"suggestedName"`
	Title             string   `json:"title,omitempty"`
	Description       string   `json:"description,omitempty"`
	Version           string   `json:"version,omitempty"`
	RepositoryURL     string   `json:"repositoryUrl,omitempty"`
	Installable       bool     `json:"installable"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
	Transport         string   `json:"transport,omitempty"`
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	URL               string   `json:"url,omitempty"`
}

type Result struct {
	Entries []Entry
	Cached  bool
	Warning string
}

func (c *Client) Search(ctx context.Context, query string, limit int) (Result, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	key := cacheKey(query, limit)
	entries, err := c.fetch(ctx, query, limit)
	if err == nil {
		c.storeCache(key, entries)
		return Result{Entries: entries}, nil
	}
	if cached, ok := c.loadCache(key); ok {
		return Result{Entries: cached, Cached: true, Warning: err.Error()}, nil
	}
	return Result{}, err
}

func (c *Client) Resolve(ctx context.Context, registryName string) (Entry, Result, error) {
	name := strings.TrimSpace(registryName)
	if name == "" {
		return Entry{}, Result{}, fmt.Errorf("registry server name is required")
	}
	// Installation must use current Registry metadata. Search deliberately falls
	// back to cache for offline browsing, but a cached package may have been
	// removed or marked unavailable since it was stored.
	entries, err := c.fetch(ctx, name, maxLimit)
	if err != nil {
		return Entry{}, Result{}, err
	}
	c.storeCache(cacheKey(name, maxLimit), entries)
	result := Result{Entries: entries}
	for _, entry := range result.Entries {
		if entry.Name == name || strings.EqualFold(entry.Name, name) {
			return entry, result, nil
		}
	}
	return Entry{}, result, fmt.Errorf("MCP Registry has no server named %q", name)
}

func (e Entry) PluginEntry(localName string) (config.PluginEntry, error) {
	if !e.Installable {
		reason := e.UnavailableReason
		if reason == "" {
			reason = "the registry entry has no directly installable transport"
		}
		return config.PluginEntry{}, fmt.Errorf("registry server %q requires manual setup: %s", e.Name, reason)
	}
	name := strings.TrimSpace(localName)
	if name == "" {
		name = SuggestedName(e.Name)
	}
	entry := config.PluginEntry{Name: name}
	switch e.Transport {
	case "stdio":
		entry.Command = e.Command
		entry.Args = append([]string(nil), e.Args...)
	case "http", "sse":
		entry.Type = e.Transport
		entry.URL = e.URL
	default:
		return config.PluginEntry{}, fmt.Errorf("registry server %q has unsupported transport %q", e.Name, e.Transport)
	}
	return entry, nil
}

var invalidLocalName = regexp.MustCompile(`[^a-z0-9._-]+`)

func SuggestedName(registryName string) string {
	name := strings.TrimSpace(registryName)
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	name = strings.ToLower(name)
	name = invalidLocalName.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-._")
	if name == "" {
		return "mcp-server"
	}
	if len(name) > 64 {
		name = strings.TrimRight(name[:64], "-._")
	}
	return name
}

type apiResponse struct {
	Servers []struct {
		Server apiServer `json:"server"`
	} `json:"servers"`
}

type apiServer struct {
	Name        string       `json:"name"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Repository  *apiRepo     `json:"repository"`
	Packages    []apiPackage `json:"packages"`
	Remotes     []apiRemote  `json:"remotes"`
}

type apiRepo struct {
	URL string `json:"url"`
}

type apiPackage struct {
	RegistryType         string            `json:"registryType"`
	Identifier           string            `json:"identifier"`
	Version              string            `json:"version"`
	Transport            apiTransport      `json:"transport"`
	EnvironmentVariables []json.RawMessage `json:"environmentVariables"`
	PackageArguments     []json.RawMessage `json:"packageArguments"`
	RuntimeArguments     []json.RawMessage `json:"runtimeArguments"`
}

type apiRemote struct {
	Type      string                     `json:"type"`
	URL       string                     `json:"url"`
	Headers   []json.RawMessage          `json:"headers"`
	Variables map[string]json.RawMessage `json:"variables"`
}

type apiTransport struct {
	Type string `json:"type"`
}

func (c *Client) fetch(ctx context.Context, query string, limit int) ([]Entry, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	endpoint, err := url.Parse(base + "/v0.1/servers")
	if err != nil {
		return nil, err
	}
	values := endpoint.Query()
	values.Set("limit", fmt.Sprintf("%d", limit))
	values.Set("version", "latest")
	if query != "" {
		values.Set("search", query)
	}
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "reasonix-mcp-registry/dev")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query MCP Registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("query MCP Registry: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var response apiResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxBody))
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode MCP Registry response: %w", err)
	}
	entries := make([]Entry, 0, len(response.Servers))
	for _, item := range response.Servers {
		if entry, ok := normalize(item.Server); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func normalize(server apiServer) (Entry, bool) {
	if strings.TrimSpace(server.Name) == "" {
		return Entry{}, false
	}
	entry := Entry{
		Name:          server.Name,
		SuggestedName: SuggestedName(server.Name),
		Title:         server.Title,
		Description:   server.Description,
		Version:       server.Version,
	}
	if server.Repository != nil {
		entry.RepositoryURL = server.Repository.URL
	}
	var reasons []string
	for _, remote := range server.Remotes {
		transport := strings.ToLower(strings.TrimSpace(remote.Type))
		if transport != "streamable-http" && transport != "http" && transport != "sse" {
			continue
		}
		if strings.TrimSpace(remote.URL) == "" {
			continue
		}
		if len(remote.Headers) > 0 || len(remote.Variables) > 0 {
			reasons = append(reasons, "remote transport requires headers or URL variables")
			continue
		}
		entry.Installable = true
		entry.Transport = "http"
		if transport == "sse" {
			entry.Transport = "sse"
		}
		entry.URL = remote.URL
		return entry, true
	}
	for _, pkg := range server.Packages {
		transport := strings.ToLower(strings.TrimSpace(pkg.Transport.Type))
		if transport != "" && transport != "stdio" {
			continue
		}
		if len(pkg.EnvironmentVariables) > 0 || len(pkg.PackageArguments) > 0 || len(pkg.RuntimeArguments) > 0 {
			reasons = append(reasons, "package requires environment variables or arguments")
			continue
		}
		identifier := strings.TrimSpace(pkg.Identifier)
		if identifier == "" {
			continue
		}
		version := strings.TrimSpace(pkg.Version)
		if version == "" {
			version = strings.TrimSpace(server.Version)
		}
		switch strings.ToLower(strings.TrimSpace(pkg.RegistryType)) {
		case "npm":
			entry.Command = "npx"
			entry.Args = []string{"-y", npmPackageVersion(identifier, version)}
		case "pypi":
			entry.Command = "uvx"
			entry.Args = []string{pythonPackageVersion(identifier, version)}
		default:
			continue
		}
		entry.Installable = true
		entry.Transport = "stdio"
		return entry, true
	}
	if len(reasons) > 0 {
		sort.Strings(reasons)
		entry.UnavailableReason = reasons[0]
	} else {
		entry.UnavailableReason = "no supported stdio, Streamable HTTP, or SSE transport"
	}
	return entry, true
}

func npmPackageVersion(identifier, version string) string {
	if version == "" {
		return identifier
	}
	return identifier + "@" + version
}

func pythonPackageVersion(identifier, version string) string {
	if version == "" {
		return identifier
	}
	return identifier + "==" + version
}

type cacheFile struct {
	// FetchedAt is retained as a read-only fallback for caches written by older
	// Reasonix versions. New writes timestamp each query independently so a
	// successful lookup cannot keep unrelated stale results alive.
	FetchedAt      time.Time            `json:"fetchedAt,omitempty"`
	QueryFetchedAt map[string]time.Time `json:"queryFetchedAt,omitempty"`
	Queries        map[string][]Entry   `json:"queries"`
}

func cacheKey(query string, limit int) string {
	return strings.ToLower(strings.TrimSpace(query)) + "\x00" + fmt.Sprintf("%d", limit)
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) loadCache(key string) ([]Entry, bool) {
	if strings.TrimSpace(c.CachePath) == "" {
		return nil, false
	}
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		return nil, false
	}
	var cache cacheFile
	if json.Unmarshal(data, &cache) != nil {
		return nil, false
	}
	entries, ok := cache.Queries[key]
	if !ok {
		return nil, false
	}
	fetchedAt := cache.QueryFetchedAt[key]
	if fetchedAt.IsZero() {
		fetchedAt = cache.FetchedAt
	}
	if fetchedAt.IsZero() || c.now().Sub(fetchedAt) > maxCacheAge {
		return nil, false
	}
	return append([]Entry(nil), entries...), ok
}

func (c *Client) storeCache(key string, entries []Entry) {
	if strings.TrimSpace(c.CachePath) == "" {
		return
	}
	cache := cacheFile{
		QueryFetchedAt: map[string]time.Time{},
		Queries:        map[string][]Entry{},
	}
	if data, err := os.ReadFile(c.CachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
		if cache.Queries == nil {
			cache.Queries = map[string][]Entry{}
		}
		if cache.QueryFetchedAt == nil {
			cache.QueryFetchedAt = map[string]time.Time{}
		}
	}
	now := c.now().UTC()
	if cache.FetchedAt.IsZero() {
		cache.FetchedAt = now
	}
	cache.QueryFetchedAt[key] = now
	cache.Queries[key] = append([]Entry(nil), entries...)
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.CachePath), 0o700) != nil {
		return
	}
	_ = fileutil.AtomicWriteFile(c.CachePath, data, 0o600)
}
