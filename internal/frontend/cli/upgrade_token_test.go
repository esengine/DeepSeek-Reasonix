package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchLatestReleaseAuthenticatesWithGitHubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_example")
	t.Setenv("GH_TOKEN", "")
	var gatewayAuth, apiAuth string

	client := &http.Client{Transport: upgradeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case cliGatewayBase + "/stable/latest.json":
			gatewayAuth = request.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		case ghAPIReleases:
			apiAuth = request.Header.Get("Authorization")
			body, err := json.Marshal([]ghRelease{completeCLIRelease("v1.9.0", false)})
			if err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
				Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
		default:
			t.Fatalf("unexpected release request: %s", request.URL)
			return nil, nil
		}
	})}

	if _, err := fetchLatestRelease(client, cliReleaseStable); err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if apiAuth != "Bearer ghp_example" {
		t.Errorf("GitHub API Authorization = %q, want the configured token", apiAuth)
	}
	if gatewayAuth != "" {
		t.Errorf("release gateway received Authorization %q; the token must not leave api.github.com", gatewayAuth)
	}
}

func TestFetchLatestReleaseWithoutTokenStaysAnonymous(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	var apiAuth string
	seen := false

	client := &http.Client{Transport: upgradeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == ghAPIReleases {
			seen = true
			apiAuth = request.Header.Get("Authorization")
			header := make(http.Header)
			header.Set("X-RateLimit-Remaining", "0")
			return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: header,
				Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}

	_, err := fetchLatestRelease(client, cliReleaseStable)
	if !seen {
		t.Fatal("GitHub API fallback was not attempted")
	}
	if apiAuth != "" {
		t.Errorf("Authorization = %q, want no header when no token is configured", apiAuth)
	}
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("rate-limited failure should name the fix, got %v", err)
	}
}
