package serve

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestGenerateToken(t *testing.T) {
	t1 := generateToken()
	t2 := generateToken()
	if t1 == t2 {
		t.Error("generateToken should produce unique values")
	}
	if len(t1) < 32 {
		t.Errorf("token too short: %d bytes", len(t1))
	}
	// Should be base64url-encoded (no +, no /).
	if strings.ContainsAny(t1, "+/") {
		t.Errorf("token contains non-base64url chars: %q", t1)
	}
}

func TestHashPassword(t *testing.T) {
	h, err := HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$2a$12$") {
		t.Errorf("unexpected bcrypt prefix: %s", h[:20])
	}
	// Same password should produce a different hash (random salt).
	h2, _ := HashPassword("test-password")
	if h == h2 {
		t.Error("same password should produce different hash")
	}
}

func TestAuthGateModeNone(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{}) // default: authNone
	if ag.Mode() != "none" {
		t.Errorf("mode = %q, want none", ag.Mode())
	}
	if ag.Token() != "" {
		t.Errorf("token = %q, want empty", ag.Token())
	}

	// In none mode, requests pass through.
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestNormalizeAuthModeRejectsUnknown(t *testing.T) {
	if _, err := NormalizeAuthMode("tokne"); err == nil {
		t.Fatal("unknown auth mode should be rejected")
	}
}

func TestInvalidAuthModeFailsClosed(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "tokne"})
	if ag.Mode() != "invalid" {
		t.Errorf("mode = %q, want invalid", ag.Mode())
	}
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run for invalid auth mode")
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// ── Token mode tests ──

func TestTokenModeNoAuthReturns401(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTokenModeValidCookie(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/status", nil)
	req.AddCookie(&http.Cookie{Name: cookieToken, Value: "secret"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestTokenModeInvalidCookie(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/status", nil)
	req.AddCookie(&http.Cookie{Name: cookieToken, Value: "wrong"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTokenModeValidQueryParamRedirects(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})

	// Use a handler that records whether auth passed.
	var passed bool
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		passed = true
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	// Don't follow redirects so we can inspect the 302.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}

	// Check that the Set-Cookie header is present.
	setCookie := resp.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, cookieToken+"=secret") {
		t.Errorf("Set-Cookie missing token: %s", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("Set-Cookie missing HttpOnly: %s", setCookie)
	}
	if passed {
		t.Error("handler should not have run (redirected first)")
	}
}

func TestTokenModeLoopbackCookieAllowsLocalHTTP(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	c := findCookie(resp.Cookies(), cookieToken)
	if c == nil {
		t.Fatal("token cookie missing")
	}
	if c.Secure {
		t.Fatal("loopback HTTP token cookie should stay usable without Secure")
	}
}

func TestTokenModeNonLoopbackCookieAllowsPlainHTTP(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", ts.URL+"/?token=secret", nil)
	req.Host = "192.0.2.10:8787"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	c := findCookie(resp.Cookies(), cookieToken)
	if c == nil {
		t.Fatal("token cookie missing")
	}
	if c.Secure {
		t.Fatal("plain HTTP token cookie should stay usable without Secure")
	}
}

func TestTokenModeInvalidQueryParam(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTokenModeAutoGeneratesToken(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token"})
	if ag.Mode() != "token" {
		t.Errorf("mode = %q, want token", ag.Mode())
	}
	if ag.Token() == "" {
		t.Error("token should be auto-generated")
	}
	if len(ag.Token()) < 32 {
		t.Errorf("auto-generated token too short: %d", len(ag.Token()))
	}
}

// ── Password mode tests ──

func TestPasswordModeLoginPage(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("login page status = %d, want 200", resp.StatusCode)
	}
}

func TestPasswordModeNoSessionRedirects(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", ts.URL+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("redirect status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/login" {
		t.Errorf("redirect location = %q, want /login", loc)
	}
}

func TestPasswordModeAPIWithoutSessionReturns401(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	// Simulate a non-browser (fetch) request by not setting Accept: text/html.
	req, _ := http.NewRequest("GET", ts.URL+"/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API status = %d, want 401", resp.StatusCode)
	}
}

func TestPasswordModeValidLogin(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("correct")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	// Don't follow redirects so we can capture the session cookie.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"password": {"correct"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("login status = %d, want 302", resp.StatusCode)
	}

	// Extract session cookie and verify it works.
	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieSession {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie set after login")
	}

	// Use the session cookie to access a protected page.
	req, _ := http.NewRequest("GET", ts.URL+"/status", nil)
	req.AddCookie(sessionCookie)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("authenticated status = %d, want 200", resp2.StatusCode)
	}
}

func TestPasswordModeNonLoopbackHTTPLoginCookieIsUsable(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("correct")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	form := url.Values{"password": {"correct"}}
	req, _ := http.NewRequest("POST", ts.URL+"/login", strings.NewReader(form.Encode()))
	req.Host = "192.0.2.10:8787"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	sessionCookie := findCookie(resp.Cookies(), cookieSession)
	if sessionCookie == nil {
		t.Fatal("session cookie missing")
	}
	if sessionCookie.Secure {
		t.Fatal("plain HTTP password session cookie should stay usable without Secure")
	}

	req, _ = http.NewRequest("GET", ts.URL+"/status", nil)
	req.Host = "192.0.2.10:8787"
	req.AddCookie(sessionCookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated non-loopback status = %d, want 200", resp.StatusCode)
	}
}

func TestPasswordModeSanitizesRedirectCookie(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("correct")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	form := url.Values{"password": {"correct"}}
	req, _ := http.NewRequest("POST", ts.URL+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieRedirect, Value: "//evil.example/path"})

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("login status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("redirect location = %q, want /", loc)
	}
}

func TestPasswordModeWrongPassword(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("correct")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/login", url.Values{"password": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", resp.StatusCode)
	}

	// Check that no session cookie was set.
	for _, c := range resp.Cookies() {
		if c.Name == cookieSession {
			t.Error("session cookie should not be set on wrong password")
		}
	}
}

func TestPasswordModeEmptyPassword(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("correct")})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/login", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("empty password status = %d, want 401", resp.StatusCode)
	}
}

// ── Session HMAC tests ──

func TestSignAndVerifySession(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})

	tok := ag.signSession()
	if !ag.verifySession(tok) {
		t.Error("valid session should verify")
	}
}

func TestSessionVerifiesAcrossRestartWithSamePasswordHash(t *testing.T) {
	hash := mustHash("test")
	ag1 := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: hash})
	tok := ag1.signSession()

	ag2 := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: hash})
	if !ag2.verifySession(tok) {
		t.Fatal("session signed with a persisted password hash should verify after restart")
	}
}

func TestSessionRejectsDifferentPasswordHash(t *testing.T) {
	ag1 := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("first")})
	tok := ag1.signSession()

	ag2 := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("second")})
	if ag2.verifySession(tok) {
		t.Fatal("session should not verify with a different password hash")
	}
}

func TestSessionTampered(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})

	tok := ag.signSession()
	// Tamper with the payload (change a character before the dot).
	dot := strings.IndexByte(tok, '.')
	tampered := tok[:dot-1] + string(tok[dot-1]^1) + tok[dot:]
	if ag.verifySession(tampered) {
		t.Error("tampered session should not verify")
	}
}

func TestSessionWrongSignature(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})

	tok := ag.signSession()
	dot := strings.LastIndexByte(tok, '.')
	// Flip a hex character in the signature, ensuring it actually changes
	// (the first nibble might already be 0 → "0" + sig[1:] would be a no-op).
	sig := tok[dot+1:]
	target := byte('0')
	if sig[0] == target {
		target = 'f' // guaranteed different
	}
	flipped := tok[:dot+1] + string(target) + sig[1:]
	if ag.verifySession(flipped) {
		t.Error("wrong-signature session should not verify")
	}
}

func TestSessionExpired(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})

	// Craft a session with an expiry in the past.
	tok := ag.signSession()
	// The format is: expiry|nonce.sig
	pipe := strings.IndexByte(tok, '|')
	expired := "1000000000" + tok[pipe:] // Unix time in 2001
	if ag.verifySession(expired) {
		t.Error("expired session should not verify")
	}
}

func TestSessionMalformed(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "password", PasswordHash: mustHash("test")})

	for _, bad := range []string{
		"",
		"no-dot",
		"no-pipe.signature",
		".just-signature",
		"payload.",
		"payload.badhex",
	} {
		if ag.verifySession(bad) {
			t.Errorf("malformed session %q should not verify", bad)
		}
	}
}

// ── Rate limiter tests ──

func TestRateLimiterAllowsFiveThenBlocks(t *testing.T) {
	rl := newRateLimit()
	ip := "192.0.2.1"

	for i := 0; i < rateLimitMax; i++ {
		if !rl.allow(ip) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if rl.allow(ip) {
		t.Error("6th attempt should be blocked")
	}
}

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	rl := newRateLimit()
	ip := "192.0.2.2"

	// Exhaust the limit.
	for i := 0; i < rateLimitMax; i++ {
		rl.allow(ip)
	}
	if rl.allow(ip) {
		t.Error("should be blocked after exhausting limit")
	}

	// Manually expire the window.
	rl.mu.Lock()
	w := rl.attempts[ip]
	if w != nil {
		w.start = time.Now().Add(-2 * rateLimitWin)
	}
	rl.mu.Unlock()

	// Now it should be allowed again.
	if !rl.allow(ip) {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := newRateLimit()
	// Exhaust one IP.
	for i := 0; i < rateLimitMax; i++ {
		rl.allow("192.0.2.1")
	}
	// Another IP should still be allowed.
	if !rl.allow("192.0.2.2") {
		t.Error("different IP should not be rate-limited")
	}
}

// ── clientIP tests ──

func TestClientIPRemoteAddr(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x"})
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.42:12345"
	if got := ag.clientIP(req); got != "192.0.2.42" {
		t.Errorf("clientIP = %q, want 192.0.2.42", got)
	}
}

func TestClientIPIgnoresXForwardedForWithoutProxy(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x"})
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if got := ag.clientIP(req); got != "192.0.2.1" {
		t.Errorf("should use RemoteAddr without behind_proxy, got %q", got)
	}
}

func TestClientIPTrustsXForwardedForWithProxy(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x", BehindProxy: true})
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.99:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.42, 10.0.0.1")
	if got := ag.clientIP(req); got != "192.0.2.42" {
		t.Errorf("clientIP = %q, want 192.0.2.42", got)
	}
}

// ── isTLS tests ──

func TestIsTLS(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x"})
	req, _ := http.NewRequest("GET", "/", nil)
	if ag.isTLS(req) {
		t.Error("plain request should not be TLS")
	}
}

func TestIsTLSIgnoresForwardedProtoWithoutProxy(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x"})
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if ag.isTLS(req) {
		t.Error("should ignore X-Forwarded-Proto without behind_proxy")
	}
}

func TestIsTLSTrustsForwardedProtoWithProxy(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x", BehindProxy: true})
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if !ag.isTLS(req) {
		t.Error("should trust X-Forwarded-Proto with behind_proxy")
	}
}

func TestAuthCookieSecurePolicy(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x"})
	for _, host := range []string{"localhost:8787", "127.0.0.1:8787", "[::1]:8787"} {
		req, _ := http.NewRequest("GET", "http://"+host+"/", nil)
		if ag.authCookieSecure(req) {
			t.Errorf("loopback host %s should allow local HTTP cookies", host)
		}
	}

	req, _ := http.NewRequest("GET", "http://192.0.2.10:8787/", nil)
	if ag.authCookieSecure(req) {
		t.Fatal("plain HTTP cookies should stay usable without Secure")
	}

	proxy := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "x", BehindProxy: true})
	req, _ = http.NewRequest("GET", "http://example.test/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if !proxy.authCookieSecure(req) {
		t.Fatal("trusted forwarded HTTPS should mark cookies Secure")
	}
}

func TestPlainHTTPAuthWarning(t *testing.T) {
	if got := PlainHTTPAuthWarning(config.ServeConfig{AuthMode: "none"}, "0.0.0.0:8787"); got != "" {
		t.Fatalf("none auth warning = %q, want empty", got)
	}
	if got := PlainHTTPAuthWarning(config.ServeConfig{AuthMode: "password"}, "127.0.0.1:8787"); got != "" {
		t.Fatalf("loopback warning = %q, want empty", got)
	}
	if got := PlainHTTPAuthWarning(config.ServeConfig{AuthMode: "token"}, "0.0.0.0:8787"); !strings.Contains(got, "non-loopback HTTP") {
		t.Fatalf("non-loopback warning = %q, want HTTP exposure warning", got)
	}
	if got := PlainHTTPAuthWarning(config.ServeConfig{AuthMode: "password"}, ":8787"); !strings.Contains(got, "non-loopback HTTP") {
		t.Fatalf("wildcard warning = %q, want HTTP exposure warning", got)
	}
}

func TestSafeRedirectTarget(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"/", "/"},
		{"/sessions?id=abc#frag", "/sessions?id=abc"},
		{"", "/"},
		{"https://evil.example/path", "/"},
		{"//evil.example/path", "/"},
		{`/\evil.example/path`, "/"},
		{"/%2f%2fevil.example/path", "/"},
		{"/%5cevil.example/path", "/"},
		{"relative/path", "/"},
	} {
		if got := safeRedirectTarget(tc.in); got != tc.want {
			t.Errorf("safeRedirectTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── helpers ──

func mustHash(password string) string {
	h, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	return h
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}
