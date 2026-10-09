package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeErrorCarriesTheEndpointsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Invalid Authentication"))
	}))
	defer srv.Close()
	_, err := ProbeEndpoint(context.Background(), ProbeOptions{BaseURL: srv.URL, APIKey: "k"})
	var probe *ProbeError
	if !errors.As(err, &probe) || probe.Reason != ProbeUnauthorized {
		t.Fatalf("err = %v, want an unauthorized ProbeError", err)
	}
	if probe.Status != http.StatusUnauthorized || probe.Body != "Invalid Authentication" {
		t.Fatalf("status/body = %d/%q, want the endpoint's own answer", probe.Status, probe.Body)
	}
}

func TestProbeErrorWithoutAnAnswerHasNoStatusOrBody(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := ProbeEndpoint(context.Background(), ProbeOptions{BaseURL: url, APIKey: "k"})
	var probe *ProbeError
	if !errors.As(err, &probe) || probe.Reason != ProbeUnreachable {
		t.Fatalf("err = %v, want unreachable", err)
	}
	if probe.Status != 0 || probe.Body != "" {
		t.Fatalf("status/body = %d/%q, want none", probe.Status, probe.Body)
	}
}

func TestProbeNamesAnExpiredDeadlineAsATimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := ProbeEndpoint(ctx, ProbeOptions{BaseURL: srv.URL, APIKey: "k"})
	var probe *ProbeError
	if !errors.As(err, &probe) || probe.Reason != ProbeTimeout {
		t.Fatalf("err = %v, want a timeout ProbeError", err)
	}
}
