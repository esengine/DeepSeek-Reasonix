package capdiag

import (
	"strings"
	"testing"
)

func TestSanitizeErrTextRedactsSecretsAndPaths(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	in := "stdio plugin \"x\": command \"npx\" not found on PATH; PATH=\"" + home + "/bin:/usr/bin\" Bearer sk-secret-token " +
		ws + "/secret.env stderr: Authorization=Bearer abc.def"
	out := sanitizeErrTextWithPaths(in, ws, home)
	if strings.Contains(out, home) {
		t.Fatalf("home path leaked: %q", out)
	}
	if strings.Contains(out, "sk-secret") || strings.Contains(out, "abc.def") {
		t.Fatalf("token leaked: %q", out)
	}
	if strings.Contains(out, "PATH=\""+home) {
		t.Fatalf("PATH value leaked: %q", out)
	}
	if !strings.Contains(out, "<redacted>") && !strings.Contains(out, "Bearer <redacted>") {
		t.Fatalf("expected redaction markers in %q", out)
	}
}

func TestSanitizeErrTextRedactsHTTPBodyCredentialShapes(t *testing.T) {
	// An HTTP transport error carries up to 4KB of raw response body; none of
	// its credential shapes may survive into the shareable report.
	in := `http 401: {"access_token":"sk-live-secret","x-api-key":"header-secret","password":"pw-secret"} Cookie: session=cookie-secret`
	out := sanitizeErrText(in)
	for _, leaked := range []string{"sk-live-secret", "header-secret", "pw-secret", "cookie-secret"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("credential leaked %q in %q", leaked, out)
		}
	}
	if !strings.Contains(out, "http 401") {
		t.Fatalf("status context lost: %q", out)
	}
}

func TestSanitizeErrTextRedactsVendorTokenShapes(t *testing.T) {
	cases := []struct{ name, in, leaked string }{
		{"jwt", "stderr: jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.sflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c", "eyJhbGciOiJIUzI1NiJ9"},
		{"github", "stderr: fatal: ghp_abcdefghijklmnopqrstuvwxyz123456", "ghp_abcdefghijklmnopqrstuvwxyz123456"},
		{"openai", "stderr: invalid key sk-proj-abcdefghijklmnop1234", "sk-proj-abcdefghijklmnop1234"},
		{"colon-header", "http 403: x-api-key: header-secret-value", "header-secret-value"},
		{"set-cookie", "http 401: Set-Cookie: sid=abc123def456ghi; Path=/", "abc123def456ghi"},
	}
	for _, tc := range cases {
		out := sanitizeErrText(tc.in)
		if strings.Contains(out, tc.leaked) {
			t.Fatalf("%s: credential leaked in %q", tc.name, out)
		}
	}
}
