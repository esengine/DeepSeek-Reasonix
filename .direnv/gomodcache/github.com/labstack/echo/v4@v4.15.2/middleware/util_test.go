// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_matchScheme(t *testing.T) {
	tests := []struct {
		domain, pattern string
		expected        bool
	}{
		{
			domain:   "http://example.com",
			pattern:  "http://example.com",
			expected: true,
		},
		{
			domain:   "https://example.com",
			pattern:  "https://example.com",
			expected: true,
		},
		{
			domain:   "http://example.com",
			pattern:  "https://example.com",
			expected: false,
		},
		{
			domain:   "https://example.com",
			pattern:  "http://example.com",
			expected: false,
		},
	}

	for _, v := range tests {
		assert.Equal(t, v.expected, matchScheme(v.domain, v.pattern))
	}
}

func Test_matchSubdomain(t *testing.T) {
	tests := []struct {
		domain, pattern string
		expected        bool
	}{
		{
			domain:   "http://aaa.example.com",
			pattern:  "http://*.example.com",
			expected: true,
		},
		{
			domain:   "http://bbb.aaa.example.com",
			pattern:  "http://*.example.com",
			expected: true,
		},
		{
			domain:   "http://bbb.aaa.example.com",
			pattern:  "http://*.aaa.example.com",
			expected: true,
		},
		{
			domain:   "http://aaa.example.com:8080",
			pattern:  "http://*.example.com:8080",
			expected: true,
		},

		{
			domain:   "http://fuga.hoge.com",
			pattern:  "http://*.example.com",
			expected: false,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://*.aaa.example.com",
			expected: false,
		},
		{
			domain: `http://1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
      .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
      .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
      .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.example.com`,
			pattern:  "http://*.example.com",
			expected: false,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://example.com",
			expected: false,
		},
	}

	for _, v := range tests {
		assert.Equal(t, v.expected, matchSubdomain(v.domain, v.pattern))
	}
}

func TestRandomString(t *testing.T) {
	var testCases = []struct {
		name       string
		whenLength uint8
		expect     string
	}{
		{
			name:       "ok, 16",
			whenLength: 16,
		},
		{
			name:       "ok, 32",
			whenLength: 32,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uid := randomString(tc.whenLength)
			assert.Len(t, uid, int(tc.whenLength))
		})
	}
}

func TestRandomStringBias(t *testing.T) {
	t.Parallel()
	const slen = 33
	const loop = 100000

	counts := make(map[rune]int)
	var count int64

	for i := 0; i < loop; i++ {
		s := randomString(slen)
		require.Equal(t, slen, len(s))
		for _, b := range s {
			counts[b]++
			count++
		}
	}

	require.Equal(t, randomStringCharsetLen, len(counts))

	avg := float64(count) / float64(len(counts))
	for k, n := range counts {
		diff := float64(n) / avg
		if diff < 0.95 || diff > 1.05 {
			t.Errorf("Bias on '%c': expected average %f, got %d", k, avg, n)
		}
	}
}

func TestValidateOrigins(t *testing.T) {
	var testCases = []struct {
		name         string
		givenOrigins []string
		givenWhat    string
		expectErr    string
	}{
		// Valid cases
		{
			name:         "ok, empty origins",
			givenOrigins: []string{},
		},
		{
			name:         "ok, basic http",
			givenOrigins: []string{"http://example.com"},
		},
		{
			name:         "ok, basic https",
			givenOrigins: []string{"https://example.com"},
		},
		{
			name:         "ok, with port",
			givenOrigins: []string{"http://localhost:8080"},
		},
		{
			name:         "ok, with subdomain",
			givenOrigins: []string{"https://api.example.com"},
		},
		{
			name:         "ok, subdomain with port",
			givenOrigins: []string{"https://api.example.com:8080"},
		},
		{
			name:         "ok, localhost",
			givenOrigins: []string{"http://localhost"},
		},
		{
			name:         "ok, IPv4 address",
			givenOrigins: []string{"http://192.168.1.1"},
		},
		{
			name:         "ok, IPv4 with port",
			givenOrigins: []string{"http://192.168.1.1:8080"},
		},
		{
			name:         "ok, IPv6 loopback",
			givenOrigins: []string{"http://[::1]"},
		},
		{
			name:         "ok, IPv6 with port",
			givenOrigins: []string{"http://[::1]:8080"},
		},
		{
			name:         "ok, IPv6 full address",
			givenOrigins: []string{"http://[2001:db8::1]"},
		},
		{
			name:         "ok, multiple valid origins",
			givenOrigins: []string{"http://example.com", "https://api.example.com:8080"},
		},
		{
			name:         "ok, different schemes",
			givenOrigins: []string{"http://example.com", "https://example.com", "ws://example.com"},
		},
		// Invalid - missing scheme
		{
			name:         "nok, plain domain",
			givenOrigins: []string{"example.com"},
			expectErr:    "trusted origin is missing scheme or host: example.com",
		},
		{
			name:         "nok, with slashes but no scheme",
			givenOrigins: []string{"//example.com"},
			expectErr:    "trusted origin is missing scheme or host: //example.com",
		},
		{
			name:         "nok, www without scheme",
			givenOrigins: []string{"www.example.com"},
			expectErr:    "trusted origin is missing scheme or host: www.example.com",
		},
		{
			name:         "nok, localhost without scheme",
			givenOrigins: []string{"localhost:8080"},
			expectErr:    "trusted origin is missing scheme or host: localhost:8080",
		},
		// Invalid - missing host
		{
			name:         "nok, scheme only http",
			givenOrigins: []string{"http://"},
			expectErr:    "trusted origin is missing scheme or host: http://",
		},
		{
			name:         "nok, scheme only https",
			givenOrigins: []string{"https://"},
			expectErr:    "trusted origin is missing scheme or host: https://",
		},
		// Invalid - has path
		{
			name:         "nok, has simple path",
			givenOrigins: []string{"http://example.com/path"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com/path",
		},
		{
			name:         "nok, has nested path",
			givenOrigins: []string{"https://example.com/api/v1"},
			expectErr:    "trusted origin can not have path, query, and fragments: https://example.com/api/v1",
		},
		{
			name:         "nok, has root path",
			givenOrigins: []string{"http://example.com/"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com/",
		},
		// Invalid - has query
		{
			name:         "nok, has single query param",
			givenOrigins: []string{"http://example.com?foo=bar"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com?foo=bar",
		},
		{
			name:         "nok, has multiple query params",
			givenOrigins: []string{"https://example.com?foo=bar&baz=qux"},
			expectErr:    "trusted origin can not have path, query, and fragments: https://example.com?foo=bar&baz=qux",
		},
		// Invalid - has fragment
		{
			name:         "nok, has simple fragment",
			givenOrigins: []string{"http://example.com#section"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com#section",
		},
		// Invalid - combinations
		{
			name:         "nok, has path and query",
			givenOrigins: []string{"http://example.com/path?foo=bar"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com/path?foo=bar",
		},
		{
			name:         "nok, has path and fragment",
			givenOrigins: []string{"http://example.com/path#section"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com/path#section",
		},
		{
			name:         "nok, has query and fragment",
			givenOrigins: []string{"http://example.com?foo=bar#section"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com?foo=bar#section",
		},
		{
			name:         "nok, has path, query, and fragment",
			givenOrigins: []string{"http://example.com/path?foo=bar#section"},
			expectErr:    "trusted origin can not have path, query, and fragments: http://example.com/path?foo=bar#section",
		},
		// Edge cases
		{
			name:         "nok, empty string",
			givenOrigins: []string{""},
			expectErr:    "trusted origin is missing scheme or host: ",
		},
		{
			name:         "nok, whitespace only",
			givenOrigins: []string{" "},
			expectErr:    "trusted origin is missing scheme or host:  ",
		},
		{
			name:         "nok, multiple origins - first invalid",
			givenOrigins: []string{"example.com", "http://valid.com"},
			expectErr:    "trusted origin is missing scheme or host: example.com",
		},
		{
			name:         "nok, multiple origins - middle invalid",
			givenOrigins: []string{"http://valid1.com", "invalid.com", "http://valid2.com"},
			expectErr:    "trusted origin is missing scheme or host: invalid.com",
		},
		{
			name:         "nok, multiple origins - last invalid",
			givenOrigins: []string{"http://valid.com", "invalid.com"},
			expectErr:    "trusted origin is missing scheme or host: invalid.com",
		},
		// Different "what" parameter
		{
			name:         "nok, custom what parameter - missing scheme",
			givenOrigins: []string{"example.com"},
			givenWhat:    "allowed origin",
			expectErr:    "allowed origin is missing scheme or host: example.com",
		},
		{
			name:         "nok, custom what parameter - has path",
			givenOrigins: []string{"http://example.com/path"},
			givenWhat:    "cors origin",
			expectErr:    "cors origin can not have path, query, and fragments: http://example.com/path",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			what := tc.givenWhat
			if what == "" {
				what = "trusted origin"
			}
			err := validateOrigins(tc.givenOrigins, what)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
