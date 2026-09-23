// Fetch public release pages using the same Go HTTP transport as the shipped updater.
package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxPageBytes = 4 << 20

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func pageURL(kind, version string) (string, error) {
	if !stableVersion.MatchString(version) {
		return "", fmt.Errorf("invalid stable version")
	}
	switch kind {
	case "homepage":
		return "https://reasonix.io/?download=desktop&release-postflight=v" + version, nil
	case "changelog":
		return "https://reasonix.io/changelog/v" + version + "/", nil
	default:
		return "", fmt.Errorf("invalid public page kind")
	}
}

func fetchPage(client *http.Client, url, version, kind string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", fmt.Sprintf("Reasonix-Updater/v%s (%s/%s; build=stable; update=stable)", version, runtime.GOOS, runtime.GOARCH))
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("cf-mitigated") != "" {
		return nil, fmt.Errorf("public %s returned HTTP %d or a Cloudflare challenge", kind, response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("content-type"))
	if err != nil || mediaType != "text/html" {
		return nil, fmt.Errorf("public %s is not HTML", kind)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > maxPageBytes {
		return nil, fmt.Errorf("public %s has invalid size", kind)
	}
	if kind == "homepage" && !strings.Contains(string(body), `data-desktop-asset="`) {
		return nil, fmt.Errorf("public homepage is missing Desktop download entries")
	}
	if kind == "changelog" && !strings.Contains(string(body), "v"+version) {
		return nil, fmt.Errorf("public changelog is missing the requested version")
	}
	return body, nil
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: release-site-fetch KIND VERSION OUTPUT")
		os.Exit(2)
	}
	kind, version, output := os.Args[1], os.Args[2], os.Args[3]
	url, err := pageURL(kind, version)
	if err == nil {
		client := &http.Client{Timeout: 30 * time.Second}
		var body []byte
		body, err = fetchPage(client, url, version, kind)
		if err == nil {
			err = os.WriteFile(output, body, 0o600)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "public release page:", err)
		os.Exit(1)
	}
}
