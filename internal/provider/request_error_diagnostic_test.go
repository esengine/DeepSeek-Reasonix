package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestTransportDiagnosticReasonsAreTypedAndSafe(t *testing.T) {
	for _, tt := range []struct {
		name       string
		err        error
		code, kind string
	}{
		{"eof", io.EOF, "eof", "transport_eof"},
		{"truncated", io.ErrUnexpectedEOF, "unexpected_eof", "transport_eof"},
		{"reset", closedConnectionErrnos[0], "connection_reset", "transport_reset"},
		{"timeout", os.ErrDeadlineExceeded, "timeout", "transport_timeout"},
		{"h2", http2.ConnectionError(http2.ErrCodeInternal), "http2_protocol", "transport_protocol"},
		{"untyped", errors.New("EOF TLS timeout ?key=private-key"), "unknown", "unknown"},
		{"send", &RequestSendTimeoutError{Budget: requestSendTimeout}, "request_send_timeout", "request_send_timeout"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := &url.Error{Op: "POST", URL: "https://user:private-key@provider.test/?key=private-key", Err: tt.err}
			if got := transportErrorCode(err); got != tt.code {
				t.Fatalf("code=%s", got)
			}
			d := DiagnoseFailure(&RequestFailure{Identity: RequestIdentity{Provider: "deepseek"}, Err: err})
			if d.Kind != tt.kind {
				t.Fatalf("diagnostic=%+v", d)
			}
			var last RequestObservation
			ctx, s := observeRequest(WithRequestObserver(t.Context(), func(v RequestObservation) { last = v }))
			trace := httptrace.ContextClientTrace(ctx)
			trace.TLSHandshakeStart()
			trace.TLSHandshakeDone(tls.ConnectionState{}, err)
			s.finish(err, "request_error")
			if last.ErrorCode != tt.code || last.ErrorPhase != "tls" || last.LastPhase != "tls_failed" {
				t.Fatalf("observation=%+v", last)
			}
			raw, _ := json.Marshal(struct {
				Failure *FailureDiagnostic
				Request RequestObservation
			}{d, last})
			if strings.Contains(string(raw), "private-key") || strings.Contains(string(raw), "https://") {
				t.Fatalf("unsafe diagnostic=%s", raw)
			}
		})
	}
}

func TestTransportObservationTimeoutSource(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		ctx := t.Context()
		want := "unknown"
		if parentDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer cancel()
			want = "parent_context"
		}
		var last RequestObservation
		_, s := observeRequest(WithRequestObserver(ctx, func(v RequestObservation) { last = v }))
		s.finish(context.DeadlineExceeded, "request_error")
		if last.TimeoutSource != want || last.ErrorPhase != "unknown" {
			t.Fatalf("timeout source=%+v, want=%s", last, want)
		}
	}
}

func TestTransportObservationParentTimeoutAndLateWrite(t *testing.T) {
	var last RequestObservation
	parent, cancel := context.WithCancel(t.Context())
	ctx, s := observeRequest(WithRequestObserver(parent, func(v RequestObservation) { last = v }))
	trace := httptrace.ContextClientTrace(ctx)
	trace.WroteRequest(httptrace.WroteRequestInfo{Err: io.ErrUnexpectedEOF})
	cancel()
	s.finish(context.Canceled, "request_error")
	if last.Phase != "canceled" || last.ErrorCode != "cancelled" {
		t.Fatalf("cancellation=%+v", last)
	}
	_, s = observeRequest(WithRequestObserver(t.Context(), func(v RequestObservation) { last = v }))
	s.update("headers_received", func(v *RequestObservation) { v.HeadersAt = v.ObservedAt })
	s.transportFailure(io.ErrUnexpectedEOF, "request_write_failed")
	if last.Phase != "headers_received" || last.ErrorCode != "" {
		t.Fatalf("late write overwrote response=%+v", last)
	}
}

func TestTransportObservationParentWinsConcurrentBodyFailure(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		wantPhase, wantCode, wantSource := "canceled", "cancelled", ""
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			wantPhase, wantCode, wantSource = "deadline_exceeded", "timeout", "parent_context"
		}
		var last RequestObservation
		_, s := observeRequest(WithRequestObserver(ctx, func(v RequestObservation) { last = v }))
		s.update("body_received", nil)
		cancel()
		s.finish(io.EOF, "body_eof")
		if last.Phase != wantPhase || last.ErrorCode != wantCode || last.TimeoutSource != wantSource || last.ErrorPhase != "response_body" {
			t.Fatalf("caller terminal cause lost: %+v", last)
		}
	}
}

func TestFailureDiagnosticStrictPreviousReader(t *testing.T) {
	// This is the complete persisted diagnostic field set in v1.39.8/v1.39.9.
	// Required recovery events use DisallowUnknownFields, unlike optional events.
	type previous struct {
		Kind                string `json:"kind"`
		Status              int    `json:"status,omitempty"`
		TraceID             string `json:"traceId,omitempty"`
		ProviderID          string `json:"providerId,omitempty"`
		ProviderDisplayName string `json:"providerDisplayName,omitempty"`
		Protocol            string `json:"protocol,omitempty"`
		RequestPath         string `json:"requestPath,omitempty"`
		TransportCode       string `json:"transportCode,omitempty"`
	}
	for _, kind := range []string{"request_send_timeout", "transport_eof", "transport_reset", "transport_timeout", "transport_dns", FailureKindImageInput} {
		raw, _ := json.Marshal(FailureDiagnostic{Kind: kind, ProviderID: "deepseek", Protocol: "openai"})
		var old previous
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&old); err != nil || old.Kind != kind {
			t.Fatalf("previous reader: %+v %v", old, err)
		}
	}
}
