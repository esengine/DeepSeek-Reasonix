package feedback

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reasonix/internal/safety/redirectguard"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/netclient"
	"reasonix/internal/platform/crashreport"
)

const (
	defaultBase  = "https://crash.reasonix.io"
	baseEnv      = "REASONIX_FEEDBACK_URL"
	snippetRunes = 80
)

// Config wires a Service. Home is where identity and receipts are kept.
type Config struct {
	Home  string
	Base  string
	Proxy netclient.ProxySpec
	HTTP  *http.Client
	// Backoff sets retry waits; a transient submit response uses the first wait.
	// Nil takes the default, an empty non-nil slice disables retrying.
	Backoff []time.Duration
}

// Service is the one client every frontend drives.
type Service struct {
	base    string
	http    *http.Client
	store   *store
	backoff []time.Duration
	limits  Limits
	// replying holds the receipts whose reply is on its way.
	replying sync.Map
}

func New(cfg Config) (*Service, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.Base), "/")
	if base != "" && !safeBase(base) {
		return nil, fmt.Errorf("feedback: base URL %q must be https (plain http only on loopback)", base)
	}
	if env := strings.TrimRight(strings.TrimSpace(os.Getenv(baseEnv)), "/"); base == "" && safeBase(env) {
		base = env
	}
	if base == "" {
		base = defaultBase
	}
	hc := cfg.HTTP
	if hc == nil {
		var err error
		hc, err = netclient.NewHTTPClient(cfg.Proxy, netclient.TransportOptions{
			DialTimeout:           5 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		})
		if err != nil {
			return nil, err
		}
		hc.Timeout = 60 * time.Second
		hc.CheckRedirect = redirectguard.Follow(hostOf(base))
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = []time.Duration{400 * time.Millisecond, 1500 * time.Millisecond}
	}
	return &Service{base: base, http: hc, store: newStore(cfg.Home), backoff: backoff, limits: DefaultLimits}, nil
}

func (s *Service) Limits() Limits { return s.limits }

// Env is what a report from ctx would carry, for showing before it is sent.
func (s *Service) Env(ctx EnvContext) Env { return CollectEnv(ctx) }

// DisplayName is the nickname remembered on this machine, or "".
func (s *Service) DisplayName() string {
	st, err := s.store.load()
	if err != nil {
		return ""
	}
	return st.DisplayName
}

// SetDisplayName remembers the nickname reports go out under.
func (s *Service) SetDisplayName(name string) error {
	name, err := s.cleanName(name)
	if err != nil {
		return err
	}
	return s.store.update(func(st *state) error { st.DisplayName = name; return nil })
}

func (s *Service) cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid(FieldDisplayName, ReasonEmpty)
	}
	if utf8.RuneCountInString(name) > s.limits.NameChars {
		return "", invalid(FieldDisplayName, ReasonTooLong)
	}
	return crashreport.Redact(name, 4*s.limits.NameChars), nil
}

// Submit validates, redacts, and sends one report. A failure that leaves the
// outcome unknown is safe to retry: without a caller key the key of the last
// unfinished send of the same content is reused, so a rerun never files twice.
func (s *Service) Submit(ctx context.Context, d Draft) (Receipt, error) {
	wire, redacted, err := s.prepare(d)
	if err != nil {
		return Receipt{}, err
	}
	id, token, key, err := s.store.begin(strings.TrimSpace(d.IdempotencyKey), wire.fingerprint())
	if err != nil {
		return Receipt{}, err
	}
	wire.IdempotencyKey, wire.InstallID = key, id
	got, err := s.post(ctx, wire, token)
	if errors.Is(err, ErrBadToken) {
		if wire.InstallID, err = s.store.rotate(); err != nil {
			return Receipt{}, err
		}
		got, err = s.post(ctx, wire, "")
	}
	if err != nil {
		return Receipt{}, err
	}
	created := got.CreatedAt
	if created.IsZero() {
		created = now().UTC()
	}
	item := Item{
		Receipt: got.Receipt, Category: d.Category, Status: got.Status,
		TitleSnippet: snippet(wire.Body), CreatedAt: created, UpdatedAt: created,
	}
	if err := s.store.update(func(st *state) error {
		if got.InstallToken != "" {
			st.InstallToken = got.InstallToken
		}
		st.DisplayName = wire.DisplayName
		st.Pending = nil
		st.remember(item)
		return nil
	}); err != nil {
		return Receipt{}, err
	}
	return Receipt{Receipt: got.Receipt, Status: got.Status, CreatedAt: created, Redacted: redacted}, nil
}

func (s *Service) prepare(d Draft) (wireSubmit, bool, error) {
	if !d.Category.valid() {
		return wireSubmit{}, false, invalid(FieldCategory, ReasonBadValue)
	}
	body := strings.TrimSpace(d.Body)
	if body == "" {
		return wireSubmit{}, false, invalid(FieldBody, ReasonEmpty)
	}
	redactedBody := crashreport.Redact(body, 4*s.limits.BodyBytes)
	if len(redactedBody) > s.limits.BodyBytes {
		return wireSubmit{}, false, invalid(FieldBody, ReasonTooLong)
	}
	name, err := s.cleanName(d.DisplayName)
	if err != nil {
		return wireSubmit{}, false, err
	}
	contact := strings.TrimSpace(d.Contact)
	if utf8.RuneCountInString(contact) > s.limits.ContactChars {
		return wireSubmit{}, false, invalid(FieldContact, ReasonTooLong)
	}
	if !d.Env.Surface.Valid() {
		return wireSubmit{}, false, invalid(FieldCategory, ReasonBadValue)
	}
	imgs, err := Normalize(d.Images, s.limits)
	if err != nil {
		return wireSubmit{}, false, err
	}
	env := CollectEnv(d.Env)
	wire := wireSubmit{
		Category: d.Category, Body: redactedBody,
		DisplayName: name, Contact: contact, Env: env,
		TurnstileToken: strings.TrimSpace(d.TurnstileToken),
	}
	for _, a := range imgs {
		wire.Attachments = append(wire.Attachments, wireAttachment{
			Name: a.Name, ContentType: a.ContentType, DataBase64: base64.StdEncoding.EncodeToString(a.Data),
		})
	}
	return wire, redactedBody != body, nil
}

func snippet(body string) string {
	r := []rune(strings.TrimSpace(body))
	return string(r[:min(len(r), snippetRunes)])
}

// ListMine reads this install's reports. An install that never sent one has
// nothing to ask about; one that cannot reach the service answers from what it
// remembers and says so.
func (s *Service) ListMine(ctx context.Context) (Mine, error) {
	st, err := s.store.load()
	if err != nil {
		return Mine{}, err
	}
	if st.InstallID == "" || st.InstallToken == "" {
		return st.mine(false), nil
	}
	var resp struct {
		Items []Item `json:"items"`
	}
	err = s.get(ctx, st.InstallID, st.InstallToken, &resp)
	switch {
	case err == nil:
	case isOffline(err):
		return st.mine(true), nil
	case errors.Is(err, ErrBadToken):
		if err := s.store.update(func(st *state) error { st.retire(); return nil }); err != nil {
			return Mine{}, err
		}
		st, err = s.store.load()
		return st.mine(false), err
	default:
		return Mine{}, err
	}
	if err := s.store.update(func(st *state) error { st.remember(resp.Items...); return nil }); err != nil {
		return Mine{}, err
	}
	st, err = s.store.load()
	if err != nil {
		return Mine{}, err
	}
	return st.mine(false), nil
}

func isOffline(err error) bool { return errors.Is(err, ErrOffline) || errors.Is(err, ErrUnavailable) }

// safeBase accepts https, and plain http only to a loopback address for tests
// and local stubs: a report and the install token must not cross the network
// in the clear.
func safeBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
