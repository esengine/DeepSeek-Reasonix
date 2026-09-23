package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageURL(t *testing.T) {
	url, err := pageURL("changelog", "1.38.12")
	if err != nil || url != "https://reasonix.io/changelog/v1.38.12/" {
		t.Fatalf("changelog URL = %q, %v", url, err)
	}
	for _, kind := range []string{"unknown", ""} {
		if _, err := pageURL(kind, "1.38.12"); err == nil {
			t.Fatalf("accepted kind %q", kind)
		}
	}
	if _, err := pageURL("homepage", "1.38.12-preview.1"); err == nil {
		t.Fatal("accepted non-Stable version")
	}
}

func TestFetchPageRejectsChallengesAndWrongContent(t *testing.T) {
	for _, testcase := range []struct {
		status, mitigation, contentType, body string
		wantError                             bool
	}{
		{"200", "", "text/html; charset=utf-8", `<div data-desktop-asset="Reasonix.dmg">`, false},
		{"403", "challenge", "text/html", "Just a moment", true},
		{"200", "challenge", "text/html", `<div data-desktop-asset="Reasonix.dmg">`, true},
		{"200", "", "application/json", `{}`, true},
		{"200", "", "text/html", `<html>unrelated</html>`, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !strings.HasPrefix(request.Header.Get("User-Agent"), "Reasonix-Updater/v1.38.12 ") {
				t.Errorf("wrong User-Agent: %q", request.Header.Get("User-Agent"))
			}
			writer.Header().Set("Content-Type", testcase.contentType)
			if testcase.mitigation != "" {
				writer.Header().Set("cf-mitigated", testcase.mitigation)
			}
			if testcase.status == "403" {
				writer.WriteHeader(http.StatusForbidden)
			}
			_, _ = writer.Write([]byte(testcase.body))
		}))
		_, err := fetchPage(server.Client(), server.URL, "1.38.12", "homepage")
		server.Close()
		if (err != nil) != testcase.wantError {
			t.Fatalf("status %s mitigation %q body %q: error = %v", testcase.status, testcase.mitigation, testcase.body, err)
		}
	}
}
