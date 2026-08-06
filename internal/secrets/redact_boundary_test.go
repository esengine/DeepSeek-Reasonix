package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
		leak []string
	}{
		{
			name: "nil error yields empty string",
			err:  nil,
			want: "",
		},
		{
			name: "empty string error yields empty string",
			err:  errors.New(""),
			want: "",
		},
		{
			name: "plain error passes through untouched",
			err:  errors.New("connection refused"),
			want: "connection refused",
		},
		{
			name: "credential key value is fully scrubbed",
			err:  errors.New("request failed: DEEPSEEK_API_KEY=sk-real-secret-value-123456"),
			want: "request failed: DEEPSEEK_API_KEY=****",
			leak: []string{"sk-real-secret-value-123456"},
		},
		{
			name: "bearer token is fully scrubbed",
			err:  errors.New("upstream returned Authorization: Bearer abcdef0123456789abcdef"),
			want: "upstream returned Authorization: Bearer [redacted]",
			leak: []string{"abcdef0123456789abcdef"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactError(tt.err)
			if got != tt.want {
				t.Fatalf("RedactError() = %q, want %q", got, tt.want)
			}
			for _, leaked := range tt.leak {
				if strings.Contains(got, leaked) {
					t.Fatalf("credential leaked %q in %q", leaked, got)
				}
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "empty value",
			value: "",
			want:  redactedValue,
		},
		{
			name:  "whitespace only collapses to empty",
			value: "   ",
			want:  redactedValue,
		},
		{
			name:  "single character",
			value: "a",
			want:  redactedValue,
		},
		{
			name:  "short token below the 12-char gate",
			value: strings.Repeat("a", 8),
			want:  redactedValue,
		},
		{
			name:  "9-char token still below the 12-char gate",
			value: strings.Repeat("a", 9),
			want:  redactedValue,
		},
		{
			name:  "token at the 12-char cutoff",
			value: strings.Repeat("a", 12),
			want:  redactedValue,
		},
		{
			name:  "token just above the 12-char cutoff",
			value: strings.Repeat("a", 13),
			want:  "aaaa*****aaaa",
		},
		{
			name:  "long token",
			value: strings.Repeat("a", 20),
			want:  "aaaa" + strings.Repeat("*", 12) + "aaaa",
		},
		{
			name:  "surrounding whitespace is trimmed before masking",
			value: "  " + strings.Repeat("a", 13) + "  ",
			want:  "aaaa*****aaaa",
		},
		{
			name:  "sk- short token below the 12-char gate",
			value: "sk-" + strings.Repeat("a", 7),
			want:  redactedValue,
		},
		{
			name:  "sk- token at the 12-char cutoff",
			value: "sk-" + strings.Repeat("a", 9),
			want:  redactedValue,
		},
		{
			name:  "sk- token just above the 12-char cutoff",
			value: "sk-" + strings.Repeat("a", 10),
			want:  "sk-aaa***aaaa",
		},
		{
			name:  "sk- long token",
			value: "sk-" + strings.Repeat("a", 10) + strings.Repeat("b", 4),
			want:  "sk-aaa*******bbbb",
		},
		{
			name:  "rk- token just above the 12-char cutoff",
			value: "rk-" + strings.Repeat("a", 10),
			want:  "rk-aaa***aaaa",
		},
		{
			name:  "rk- long token",
			value: "rk-" + strings.Repeat("a", 7) + strings.Repeat("b", 4),
			want:  "rk-aaa****bbbb",
		},
		{
			name:  "uppercase SK- prefix is not special",
			value: "SK-" + strings.Repeat("a", 10),
			want:  "SK-a*****aaaa",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mask(tt.value); got != tt.want {
				t.Fatalf("mask(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestAuthorizationScheme(t *testing.T) {
	for _, scheme := range []string{
		"Bearer", "bearer", "BEARER",
		"Basic", "basic", "BASIC",
		"Digest", "digest",
		"Negotiate", "negotiate",
		"NTLM", "ntlm",
		"Token", "token",
		"Bot", "bot",
		"APIKey", "apikey", "ApiKey",
	} {
		if !authorizationScheme(scheme) {
			t.Errorf("authorizationScheme(%q) = false, want true", scheme)
		}
	}
	for _, scheme := range []string{
		"", "Bearer ", " Bearer", "bearerX", "Basic-auth", "basic-auth",
		"none", "custom", "x-bearer", "AWS4-HMAC-SHA256", "Bearer\n",
	} {
		if authorizationScheme(scheme) {
			t.Errorf("authorizationScheme(%q) = true, want false", scheme)
		}
	}
}

func TestCredentialKeyByte(t *testing.T) {
	for _, b := range []byte{'a', 'z', 'A', 'Z', '0', '9', '_', '-', '.'} {
		if !credentialKeyByte(b) {
			t.Errorf("credentialKeyByte(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{' ', '\t', '\n', '\r', '\f', '\v', ':', '=', '@', '/', '!', '~', '\'', '"', ',', ';', '#', '$', '?', '&', 0x7f} {
		if credentialKeyByte(b) {
			t.Errorf("credentialKeyByte(%q) = true, want false", b)
		}
	}
	// Every byte above ASCII must also be rejected.
	for b := 0x80; b <= 0xff; b++ {
		if credentialKeyByte(byte(b)) {
			t.Errorf("credentialKeyByte(0x%02x) = true, want false", b)
		}
	}
}

func TestASCIISpace(t *testing.T) {
	for _, b := range []byte{' ', '\t', '\n', '\r', '\f'} {
		if !asciiSpace(b) {
			t.Errorf("asciiSpace(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{'\v', 0x00, 'a', '\'', '0', 0x85, 0xa0, 0x1c, 0x7f} {
		if asciiSpace(b) {
			t.Errorf("asciiSpace(0x%02x) = true, want false", b)
		}
	}
}
