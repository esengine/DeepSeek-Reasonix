package bot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const DefaultOutboundMediaLimit int64 = 20 << 20

type MediaPolicy struct {
	LocalRoots []string
	MaxBytes   int64
	AllowHosts map[string]bool
	ResolveDNS bool
}

type PreparedMedia struct {
	Kind   string
	Name   string
	MIME   string
	Size   int64
	SHA256 string
	Data   []byte
}

var mediaLookupIP = net.DefaultResolver.LookupIP

func pinnedMediaTransport(policy MediaPolicy) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		host = strings.TrimSpace(host)
		if len(policy.AllowHosts) > 0 && !policy.AllowHosts[strings.ToLower(host)] {
			return nil, fmt.Errorf("outbound media host is not allowlisted")
		}
		var ips []net.IP
		if literal := net.ParseIP(host); literal != nil {
			ips = []net.IP{literal}
		} else {
			ips, err = mediaLookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve media host: %w", err)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("media host has no IP addresses")
		}
		if slices.ContainsFunc(ips, isPrivateMediaIP) {
			return nil, fmt.Errorf("media host resolves to a private address")
		}
		dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 15 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return transport
}

func (m PreparedMedia) Open() io.ReadCloser { return io.NopCloser(bytes.NewReader(m.Data)) }

// PrepareOutboundMedia resolves and validates a media reference in the host.
// Adapters receive only immutable bytes and never fetch arbitrary URLs or
// local paths themselves.
func PrepareOutboundMedia(ctx context.Context, media OutboundMedia, policy MediaPolicy) (PreparedMedia, error) {
	if err := validateOutboundMediaShape(media, policy); err != nil {
		return PreparedMedia{}, err
	}
	limit := policy.MaxBytes
	if limit <= 0 {
		limit = DefaultOutboundMediaLimit
	}
	data := append([]byte(nil), media.Data...)
	if len(data) == 0 && strings.TrimSpace(media.Path) != "" {
		var err error
		data, err = readAllowedOutboundMedia(media.Path, policy.LocalRoots, limit)
		if err != nil {
			return PreparedMedia{}, err
		}
	}
	if len(data) == 0 && strings.TrimSpace(media.URL) != "" {
		var err error
		data, err = fetchOutboundMedia(ctx, media.URL, policy, limit)
		if err != nil {
			return PreparedMedia{}, err
		}
	}
	if len(data) == 0 {
		return PreparedMedia{}, fmt.Errorf("outbound media has no readable data")
	}
	if int64(len(data)) > limit {
		return PreparedMedia{}, fmt.Errorf("outbound media exceeds %d bytes", limit)
	}
	name := strings.TrimSpace(media.Name)
	if name == "" && media.Path != "" {
		name = filepath.Base(media.Path)
	}
	mimeType := http.DetectContentType(data)
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}
	if err := validateDetectedMediaKind(media.Kind, mimeType); err != nil {
		return PreparedMedia{}, err
	}
	sum := sha256.Sum256(data)
	return PreparedMedia{Kind: strings.ToLower(strings.TrimSpace(media.Kind)), Name: name, MIME: mimeType, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sum[:]), Data: data}, nil
}

func fetchOutboundMedia(ctx context.Context, rawURL string, policy MediaPolicy, limit int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	current := rawURL
	client := &http.Client{Timeout: 30 * time.Second, Transport: pinnedMediaTransport(policy), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many outbound media redirects")
		}
		if err := validateRemoteMediaURL(req.URL, policy); err != nil {
			return err
		}
		return nil
	}}
	for range 4 {
		u, err := url.Parse(current)
		if err != nil {
			return nil, err
		}
		if err := validateRemoteMediaURL(u, policy); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			_ = resp.Body.Close()
			if location == "" {
				return nil, fmt.Errorf("outbound media redirect has no location")
			}
			next, err := u.Parse(location)
			if err != nil {
				return nil, err
			}
			current = next.String()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("outbound media download error %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("outbound media exceeds %d bytes", limit)
		}
		return data, nil
	}
	return nil, fmt.Errorf("outbound media redirect limit exceeded")
}

func validateRemoteMediaURL(u *url.URL, policy MediaPolicy) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid outbound media URL")
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if len(policy.AllowHosts) > 0 && !policy.AllowHosts[host] {
		return fmt.Errorf("outbound media host is not allowlisted")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateMediaIP(ip) {
		return fmt.Errorf("outbound media URL resolves to a private address")
	}
	if policy.ResolveDNS {
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("resolve outbound media host: %w", err)
		}
		if slices.ContainsFunc(ips, isPrivateMediaIP) {
			return fmt.Errorf("outbound media host resolves to a private address")
		}
	}
	return nil
}

func ValidateOutboundMedia(media OutboundMedia, policy MediaPolicy) error {
	if err := validateOutboundMediaShape(media, policy); err != nil {
		return err
	}
	limit := policy.MaxBytes
	if limit <= 0 {
		limit = DefaultOutboundMediaLimit
	}
	if raw := strings.TrimSpace(media.URL); raw != "" {
		u, _ := url.Parse(raw)
		return validateRemoteMediaURL(u, policy)
	}
	if raw := strings.TrimSpace(media.Path); raw != "" {
		f, err := openAllowedOutboundMedia(raw, policy.LocalRoots, limit)
		if err != nil {
			return err
		}
		return f.Close()
	}
	return nil
}

func validateOutboundMediaShape(media OutboundMedia, policy MediaPolicy) error {
	switch strings.ToLower(strings.TrimSpace(media.Kind)) {
	case "image", "file", "audio", "video":
	default:
		return fmt.Errorf("unsupported outbound media kind %q", media.Kind)
	}
	limit := policy.MaxBytes
	if limit <= 0 {
		limit = DefaultOutboundMediaLimit
	}
	if len(media.Data) > int(limit) {
		return fmt.Errorf("outbound media exceeds %d bytes", limit)
	}
	sources := 0
	if len(media.Data) > 0 {
		sources++
	}
	if strings.TrimSpace(media.URL) != "" {
		sources++
	}
	if strings.TrimSpace(media.Path) != "" {
		sources++
	}
	if sources == 0 {
		return fmt.Errorf("outbound media has no data, URL, or path")
	}
	if sources != 1 {
		return fmt.Errorf("outbound media must use exactly one data source")
	}
	if raw := strings.TrimSpace(media.URL); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Hostname() == "" {
			return fmt.Errorf("invalid outbound media URL")
		}
		host := strings.ToLower(u.Hostname())
		if len(policy.AllowHosts) > 0 && !policy.AllowHosts[host] {
			return fmt.Errorf("outbound media host is not allowlisted")
		}
		if ip := net.ParseIP(host); ip != nil && isPrivateMediaIP(ip) {
			return fmt.Errorf("outbound media URL resolves to a private address")
		}
		if policy.ResolveDNS {
			ips, err := net.LookupIP(host)
			if err != nil {
				return fmt.Errorf("resolve outbound media host: %w", err)
			}
			if slices.ContainsFunc(ips, isPrivateMediaIP) {
				return fmt.Errorf("outbound media host resolves to a private address")
			}
		}
		return nil
	}
	if raw := strings.TrimSpace(media.Path); raw != "" {
		if len(policy.LocalRoots) == 0 {
			return fmt.Errorf("outbound media path is outside allowed roots")
		}
		return nil
	}
	return nil
}

func validateDetectedMediaKind(kind, mimeType string) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch kind {
	case "image":
		if !strings.HasPrefix(mimeType, "image/") {
			return fmt.Errorf("outbound image content has MIME %q", mimeType)
		}
	case "audio":
		if !strings.HasPrefix(mimeType, "audio/") && mimeType != "application/ogg" {
			return fmt.Errorf("outbound audio content has MIME %q", mimeType)
		}
	case "video":
		if !strings.HasPrefix(mimeType, "video/") && mimeType != "application/ogg" {
			return fmt.Errorf("outbound video content has MIME %q", mimeType)
		}
	}
	return nil
}

func openAllowedOutboundMedia(path string, roots []string, limit int64) (*os.File, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	for _, allowedRoot := range roots {
		rootPath, rootErr := filepath.Abs(strings.TrimSpace(allowedRoot))
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(rootPath, absPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			continue
		}
		root, openErr := os.OpenRoot(rootPath)
		if openErr != nil {
			continue
		}
		f, openErr := root.Open(rel)
		_ = root.Close()
		if openErr != nil {
			continue
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > limit {
			_ = f.Close()
			if statErr != nil {
				return nil, statErr
			}
			return nil, fmt.Errorf("outbound media file is invalid or too large")
		}
		return f, nil
	}
	return nil, fmt.Errorf("outbound media path is outside allowed roots")
}

func readAllowedOutboundMedia(path string, roots []string, limit int64) ([]byte, error) {
	f, err := openAllowedOutboundMedia(path, roots, limit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("outbound media exceeds %d bytes", limit)
	}
	return data, nil
}

func isPrivateMediaIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func ReadOutboundMedia(path string, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = DefaultOutboundMediaLimit
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("outbound media exceeds %d bytes", limit)
	}
	return data, nil
}
