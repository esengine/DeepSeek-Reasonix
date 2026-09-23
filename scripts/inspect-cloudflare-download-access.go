package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: inspect-cloudflare-download-access.go VERSION...")
		os.Exit(2)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, version := range os.Args[1:] {
		if !stableVersion.MatchString(version) {
			fmt.Fprintln(os.Stderr, "invalid Stable version")
			os.Exit(2)
		}
		request, err := http.NewRequest(http.MethodGet, "https://dl.reasonix.io/latest/latest.json", nil)
		if err != nil {
			panic(err)
		}
		request.Header.Set("User-Agent", fmt.Sprintf("Reasonix-Updater/v%s (%s/%s; build=stable; update=stable)", version, runtime.GOOS, runtime.GOARCH))
		response, err := client.Do(request)
		if err != nil {
			panic(err)
		}
		result := map[string]any{
			"kind": "go-updater-manifest", "version": version,
			"status": response.StatusCode, "mitigation": response.Header.Get("cf-mitigated"),
			"ray": response.Header.Get("cf-ray"), "contentType": response.Header.Get("content-type"),
		}
		response.Body.Close()
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			panic(err)
		}
	}
	for _, target := range []struct{ kind, url string }{
		{"homepage", "https://reasonix.io/?download=desktop&release-postflight=v1.38.12"},
		{"changelog", "https://reasonix.io/changelog/v1.38.12/"},
	} {
		for _, profile := range []struct{ kind, userAgent string }{
			{"updater", fmt.Sprintf("Reasonix-Updater/v1.38.12 (%s/%s; build=stable; update=stable)", runtime.GOOS, runtime.GOARCH)},
			{"browser", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"},
		} {
			request, err := http.NewRequest(http.MethodGet, target.url, nil)
			if err != nil {
				panic(err)
			}
			request.Header.Set("User-Agent", profile.userAgent)
			response, err := client.Do(request)
			if err != nil {
				panic(err)
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
			response.Body.Close()
			if err != nil {
				panic(err)
			}
			result := map[string]any{
				"kind": "go-site-" + target.kind, "profile": profile.kind,
				"status": response.StatusCode, "mitigation": response.Header.Get("cf-mitigated"),
				"contentType":     response.Header.Get("content-type"),
				"hasVersion":      strings.Contains(string(body), "v1.38.12"),
				"hasDesktopAsset": strings.Contains(string(body), "data-desktop-asset="),
			}
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				panic(err)
			}
		}
	}
}
