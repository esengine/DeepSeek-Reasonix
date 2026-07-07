package serve

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"reasonix/internal/config"
)

//go:embed login.html
var loginHTML []byte

// authMode represents the authentication mode for the serve frontend.
type authMode int

const (
	authInvalid  authMode = iota // invalid config; deny all requests
	authNone                     // no authentication (default, backward-compatible)
	authToken                    // pre-shared token in URL or cookie
	authPassword                 // login page with bcrypt password
)

const (
	cookieToken     = "reasonix_token"    // holds the token for token mode
	cookieSession   = "reasonix_session"  // holds the HMAC-signed session for password mode
	cookieRedirect  = "reasonix_redirect" // temporary: where to go after login
	tokenByteLen    = 32                  // 256-bit random token
	sessionDuration = 30 * 24 * time.Hour // how long a password session lasts
	bcryptCost      = 12                  // bcrypt cost factor
	pbkdf2Iter      = 4096                // deterministic session-key derivation from password_hash
)

// NormalizeAuthMode normalizes and validates the serve auth mode.
func NormalizeAuthMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "none"
	}
	switch mode {
	case "none", "token", "password":
		return mode, nil
	default:
		return "", fmt.Errorf("auth mode must be none, token, or password, got %q", mode)
	}
}

// rateLimit tracks login attempts per IP for brute-force protection.
type rateLimit struct {
	mu       sync.Mutex
	attempts map[string]*rateWindow
}

type rateWindow struct {
	count int
	start time.Time
}

const (
	rateLimitMax = 5
	rateLimitWin = time.Minute
)

func newRateLimit() *rateLimit {
	rl := &rateLimit{attempts: make(map[string]*rateWindow)}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically purges expired rate-limit windows so the map does not
// grow without bound over the lifetime of a long-running server.
func (rl *rateLimit) cleanupLoop() {
	ticker := time.NewTicker(2 * rateLimitWin)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, w := range rl.attempts {
			if now.Sub(w.start) > rateLimitWin {
				delete(rl.attempts, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// allow reports whether the IP is allowed to attempt login. It also cleans up
// expired windows.
func (rl *rateLimit) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	w, ok := rl.attempts[ip]
	if !ok || now.Sub(w.start) > rateLimitWin {
		rl.attempts[ip] = &rateWindow{count: 1, start: now}
		return true
	}
	w.count++
	return w.count <= rateLimitMax
}

// authGate is the authentication middleware and its runtime state.
type authGate struct {
	mode         authMode
	token        string     // pre-shared token (token mode)
	passwordHash string     // bcrypt hash for password verification (password mode)
	sessKey      []byte     // HMAC key for session signing (password mode, generated at startup)
	behindProxy  bool       // trust X-Forwarded-For / X-Forwarded-Proto headers
	rateLimit    *rateLimit // per-IP rate limiter for /login
}

// newAuthGate creates the auth middleware from the serve config. For token mode
// without a configured token, it generates a random one.
func newAuthGate(cfg config.ServeConfig) *authGate {
	ag := &authGate{
		rateLimit:   newRateLimit(),
		behindProxy: cfg.BehindProxy,
	}
	mode, err := NormalizeAuthMode(cfg.AuthMode)
	if err != nil {
		ag.mode = authInvalid
		return ag
	}
	switch mode {
	case "token":
		ag.mode = authToken
		ag.token = strings.TrimSpace(cfg.Token)
		if ag.token == "" {
			ag.token = generateToken()
		}
	case "password":
		ag.mode = authPassword
		ag.passwordHash = strings.TrimSpace(cfg.PasswordHash)
		ag.sessKey = sessionKeyForPasswordHash(ag.passwordHash)
	default:
		ag.mode = authNone
	}
	return ag
}

// Token returns the shared token (empty if not in token mode).
func (ag *authGate) Token() string { return ag.token }

// Mode returns the auth mode name as a string.
func (ag *authGate) Mode() string {
	switch ag.mode {
	case authToken:
		return "token"
	case authPassword:
		return "password"
	case authInvalid:
		return "invalid"
	default:
		return "none"
	}
}

// HashPassword returns a bcrypt hash of the given password. Exported for use by
// the CLI `--hash-password` flag.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sessionKeyForPasswordHash(passwordHash string) []byte {
	if passwordHash != "" {
		key, err := pbkdf2.Key(sha256.New, passwordHash, []byte("reasonix serve session key"), pbkdf2Iter, 32)
		if err != nil {
			panic("serve/auth: pbkdf2 failed: " + err.Error())
		}
		return key
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand.Read cannot fail on modern systems; panic rather than
		// fall back to a deterministic key that would weaken every session.
		panic("serve/auth: crypto/rand.Read failed: " + err.Error())
	}
	return key
}

// middleware returns an http.Handler that wraps next with authentication checks.
// In password mode, /login is handled directly to bypass the CSRF content-type
// guard (the login form uses application/x-www-form-urlencoded, not JSON).
func (ag *authGate) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ag.mode == authInvalid {
			ag.deny(w, r)
			return
		}
		if ag.mode == authNone {
			next.ServeHTTP(w, r)
			return
		}
		// /login and /login/ are handled directly by the auth gate — they must
		// not pass through the CSRF guard (which rejects non-JSON POSTs).
		if r.URL.Path == "/login" || r.URL.Path == "/login/" {
			ag.handleLogin(w, r)
			return
		}
		if ag.mode == authToken {
			ag.checkToken(w, r, next)
			return
		}
		// password mode
		ag.checkSession(w, r, next)
	})
}

// checkToken validates the token from cookie or query parameter. If the query
// parameter is valid, it sets a cookie and redirects to strip the token from the
// URL (preventing it from leaking via browser history or referrer headers).
func (ag *authGate) checkToken(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// 1. Check cookie first (fast path).
	if c, err := r.Cookie(cookieToken); err == nil && strings.TrimSpace(c.Value) != "" {
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(ag.token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
	}

	// 2. Check query parameter.
	if q := r.URL.Query().Get("token"); q != "" {
		if subtle.ConstantTimeCompare([]byte(q), []byte(ag.token)) == 1 {
			// Set a persistent cookie so future requests (including SSE) are
			// authenticated without the token in the URL.
			ag.setAuthCookie(w, r, &http.Cookie{
				Name:     cookieToken,
				Value:    ag.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int(sessionDuration.Seconds()),
			})
			// Redirect to the same path without the token query parameter.
			cleanURL := *r.URL
			qry := cleanURL.Query()
			qry.Del("token")
			cleanURL.RawQuery = qry.Encode()
			if cleanURL.RawQuery == "" {
				cleanURL.RawQuery = ""
			}
			redirectToSafeTarget(w, r, cleanURL.RequestURI(), http.StatusFound)
			return
		}
	}

	// 3. Not authenticated.
	ag.deny(w, r)
}

// checkSession validates the HMAC-signed session cookie for password mode.
// Unauthenticated browser requests are redirected to /login; API/SSE requests
// get a 401. The /login path is intercepted before this function by middleware.
func (ag *authGate) checkSession(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// Check session cookie.
	if c, err := r.Cookie(cookieSession); err == nil {
		if ag.verifySession(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
	}

	// Not authenticated.
	if acceptsHTML(r) {
		// Store the original path so we can redirect back after login.
		dest := safeRedirectTarget(r.URL.RequestURI())
		ag.setAuthCookie(w, r, &http.Cookie{
			Name:     cookieRedirect,
			Value:    dest,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   300, // 5 minutes
		})
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	ag.deny(w, r)
}

// deny sends a 401 response. The message is intentionally generic to avoid
// leaking information about which auth mode is active.
func (ag *authGate) deny(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("Unauthorized\n"))
}

// handleLogin serves the login page (GET) or processes a login attempt (POST).
func (ag *authGate) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ag.loginPage(w, r)
	case http.MethodPost:
		ag.loginSubmit(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// loginPage serves the embedded login HTML.
func (ag *authGate) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(loginHTML)
}

// loginSubmit verifies the password and issues a session cookie.
func (ag *authGate) loginSubmit(w http.ResponseWriter, r *http.Request) {
	// Rate limit.
	ip := ag.clientIP(r)
	if !ag.rateLimit.allow(ip) {
		slog.Warn("serve/auth: rate-limited login attempt", "ip", ip)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too many attempts. Please wait a minute.\n"))
		return
	}

	// Parse the password from the form.
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	if password == "" {
		ag.loginPageWithError(w, "Password is required.")
		return
	}

	// Verify against the stored bcrypt hash.
	if ag.passwordHash == "" {
		slog.Error("serve/auth: cannot verify password — no password_hash configured")
		ag.loginPageWithError(w, "Server not configured for password authentication.")
		return
	}

	// Verify against bcrypt hash.
	if err := bcrypt.CompareHashAndPassword([]byte(ag.passwordHash), []byte(password)); err != nil {
		ag.loginPageWithError(w, "Invalid password.")
		return
	}

	// Create and sign a session.
	session := ag.signSession()

	// Clear the redirect cookie and set the session cookie.
	ag.setAuthCookie(w, r, &http.Cookie{
		Name:     cookieRedirect,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	ag.setAuthCookie(w, r, &http.Cookie{
		Name:     cookieSession,
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	// Redirect to the original destination, or /.
	dest := "/"
	if c, err := r.Cookie(cookieRedirect); err == nil && c.Value != "" {
		dest = safeRedirectTarget(c.Value)
	}
	redirectToSafeTarget(w, r, dest, http.StatusFound)
}

func (ag *authGate) setAuthCookie(w http.ResponseWriter, r *http.Request, c *http.Cookie) {
	c.Secure = ag.authCookieSecure(r)
	// codeql[go/cookie-secure-not-set] Secure cookies are only sent back over HTTPS; plain-HTTP serve must keep token/password auth usable.
	http.SetCookie(w, c)
}

func (ag *authGate) authCookieSecure(r *http.Request) bool {
	return ag.isTLS(r)
}

func safeRedirectTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\\", "/")
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return "/"
	}
	if raw != "/" && (len(raw) <= 1 || raw[0] != '/' || raw[1] == '/' || raw[1] == '\\') {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.IsAbs() || u.Hostname() != "" {
		return "/"
	}
	path := strings.ReplaceAll(u.Path, "\\", "/")
	if path == "" {
		return "/"
	}
	if path != "/" && (len(path) <= 1 || path[0] != '/' || path[1] == '/' || path[1] == '\\') {
		return "/"
	}
	return u.RequestURI()
}

func redirectToSafeTarget(w http.ResponseWriter, r *http.Request, raw string, status int) {
	target := safeRedirectTarget(raw)
	target = strings.ReplaceAll(target, "\\", "/")
	u, err := url.Parse(target)
	if err == nil && u != nil && !u.IsAbs() && u.Hostname() == "" {
		redirect := u.RequestURI()
		if redirect == "/" {
			http.Redirect(w, r, "/", status)
			return
		}
		if len(redirect) > 1 && redirect[0] == '/' && redirect[1] != '/' && redirect[1] != '\\' {
			http.Redirect(w, r, redirect, status)
			return
		}
	}
	http.Redirect(w, r, "/", status)
}

// signSession creates a new HMAC-signed session token valid for sessionDuration.
// Format: base64url(expiry_base10|random_16_bytes).hex(hmac_sha256)
func (ag *authGate) signSession() string {
	expiry := time.Now().Add(sessionDuration).Unix()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		// crypto/rand.Read cannot fail on modern systems; panic rather than
		// fall back to an all-zero nonce. Forging a cookie still requires the
		// PBKDF2-derived sessKey, so this is not an auth bypass, but a constant
		// nonce weakens session token uniqueness/unpredictability and is the
		// same anti-pattern generateToken/sessionKeyForPasswordHash panic on.
		panic("serve/auth: crypto/rand.Read failed: " + err.Error())
	}

	payload := strconv.FormatInt(expiry, 10) + "|" + base64.RawURLEncoding.EncodeToString(nonce)
	mac := hmac.New(sha256.New, ag.sessKey)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return payload + "." + sig
}

// verifySession checks that a session token is valid (HMAC matches and not expired).
func (ag *authGate) verifySession(token string) bool {
	// Split payload.signature
	dot := strings.LastIndexByte(token, '.')
	if dot < 0 {
		return false
	}
	payload, sigHex := token[:dot], token[dot+1:]

	// Verify HMAC (constant-time via hmac.Equal; handles length mismatch
	// internally so we don't leak timing information from a pre-check).
	mac := hmac.New(sha256.New, ag.sessKey)
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	if !hmac.Equal(sig, expected) {
		return false
	}

	// Check expiry (format: "unix_timestamp|base64nonce").
	pipe := strings.IndexByte(payload, '|')
	if pipe < 0 {
		return false
	}
	expiry, err := strconv.ParseInt(payload[:pipe], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < expiry
}

// loginPageWithError renders the login page with an error message.
func (ag *authGate) loginPageWithError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	html := strings.Replace(string(loginHTML), "<!--ERROR-->",
		`<div class="error">`+htmlEscape(msg)+`</div>`, 1)
	_, _ = w.Write([]byte(html))
}

// htmlEscape does minimal escaping for display in an HTML context.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&#34;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// generateToken returns a cryptographically random URL-safe token.
func generateToken() string {
	b := make([]byte, tokenByteLen)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failure is fatal for token generation.
		panic("serve/auth: crypto/rand.Read failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// acceptsHTML reports whether the request's Accept header prefers text/html.
func acceptsHTML(r *http.Request) bool {
	for _, h := range strings.Fields(r.Header.Get("Accept")) {
		if strings.HasPrefix(h, "text/html") {
			return true
		}
	}
	return false
}

// clientIP extracts the client IP from the request. When behindProxy is true,
// it trusts the leftmost entry in X-Forwarded-For (set by a trusted reverse
// proxy). Otherwise it uses RemoteAddr directly — X-Forwarded-For is ignored
// because an attacker can forge it.
func (ag *authGate) clientIP(r *http.Request) string {
	if ag.behindProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if i := strings.IndexByte(fwd, ','); i >= 0 {
				return strings.TrimSpace(fwd[:i])
			}
			return strings.TrimSpace(fwd)
		}
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[:i]
	}
	return addr
}

// isTLS reports whether the request arrived over TLS. It trusts
// X-Forwarded-Proto only when behindProxy is true.
func (ag *authGate) isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if ag.behindProxy {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}

func isLoopbackHost(hostport string) bool {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return false
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PlainHTTPAuthWarning reports whether serve is exposing authenticated access
// over a non-loopback plain-HTTP listener. The listener may still be valid for a
// trusted LAN or reverse-proxy setup, but users should see the risk explicitly.
func PlainHTTPAuthWarning(cfg config.ServeConfig, addr string) string {
	mode, err := NormalizeAuthMode(cfg.AuthMode)
	if err != nil || mode == "none" || isLoopbackHost(addr) {
		return ""
	}
	return "warning: authenticated serve is listening on non-loopback HTTP; use HTTPS via a trusted reverse proxy or bind to 127.0.0.1 for local-only access"
}
