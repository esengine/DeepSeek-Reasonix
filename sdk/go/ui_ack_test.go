package extension

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestHostUIRequestRequiresBooleanCancelled(t *testing.T) {
	for _, tc := range []struct {
		name          string
		raw           string
		protocolError bool
		cancelled     bool
		values        map[string]any
	}{
		{name: "missing", raw: `{}`, protocolError: true},
		{name: "missing with answer", raw: `{"values":{"value":true}}`, protocolError: true},
		{name: "null flag", raw: `{"cancelled":null,"values":{"value":true}}`, protocolError: true},
		{name: "null result", raw: `null`, protocolError: true},
		{name: "string flag", raw: `{"cancelled":"false"}`, protocolError: true},
		{name: "numeric flag", raw: `{"cancelled":0}`, protocolError: true},
		{name: "dismissed", raw: `{"cancelled":true}`, cancelled: true},
		{name: "empty answer", raw: `{"cancelled":false}`},
		{name: "false answer", raw: `{"cancelled":false,"values":{"value":false}}`, values: map[string]any{"value": false}},
		{name: "text answer", raw: `{"cancelled":false,"values":{"value":"text"}}`, values: map[string]any{"value": "text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			var callErr error
			host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: map[string]InterceptorFunc{
				"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
					got, callErr = (HostUI{}).RequestForm(ctx, "sess-1", 7, "form-1", UIFormPayload{Fields: []UIFormField{}})
					return Continue(), nil
				},
			}})
			host.onRequest(MethodHostUIRequest, func(json.RawMessage) (any, *hostError) { return json.RawMessage(tc.raw), nil })
			host.handshake(t)
			if resp := host.request(MethodExtensionIntercept, InterceptParams{Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`)}); resp.Err != nil {
				t.Fatal(resp.Err)
			}
			var protocolErr *ProtocolError
			if tc.protocolError {
				if !errors.As(callErr, &protocolErr) || protocolErr.Reason != ErrProtocolError {
					t.Fatalf("invalid host result returned values=%v err=%v; want protocol_error", got, callErr)
				}
				if got != nil {
					t.Errorf("invalid result returned answers: %v", got)
				}
			} else if tc.cancelled {
				if !errors.Is(callErr, ErrUICancelled) {
					t.Fatalf("dismissal error=%v", callErr)
				}
			} else if callErr != nil || !reflect.DeepEqual(got, tc.values) {
				t.Fatalf("answer=%v err=%v, want %v", got, callErr, tc.values)
			}
			if raw := host.lastRawParams(t, MethodHostUIRequest); len(raw) == 0 {
				t.Fatal("no UI request reached host")
			}
		})
	}
}

func TestHostUIPublishRequiresBooleanAccepted(t *testing.T) {
	for _, tc := range []struct {
		name          string
		raw           string
		protocolError bool
		rejected      bool
	}{
		{name: "missing", raw: `{}`, protocolError: true},
		{name: "null flag", raw: `{"accepted":null}`, protocolError: true},
		{name: "null result", raw: `null`, protocolError: true},
		{name: "string flag", raw: `{"accepted":"true"}`, protocolError: true},
		{name: "numeric flag", raw: `{"accepted":1}`, protocolError: true},
		{name: "accepted", raw: `{"accepted":true}`},
		{name: "rejected", raw: `{"accepted":false}`, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var callErr error
			host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: map[string]InterceptorFunc{
				"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
					callErr = (HostUI{}).PublishStatus(ctx, "sess-1", 7, "status-1", UIStatusPayload{Label: "Ready"})
					return Continue(), nil
				},
			}})
			host.onRequest(MethodHostUIPublish, func(json.RawMessage) (any, *hostError) { return json.RawMessage(tc.raw), nil })
			host.handshake(t)
			if resp := host.request(MethodExtensionIntercept, InterceptParams{Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`)}); resp.Err != nil {
				t.Fatal(resp.Err)
			}
			var protocolErr *ProtocolError
			if tc.protocolError {
				if !errors.As(callErr, &protocolErr) || protocolErr.Reason != ErrProtocolError {
					t.Fatalf("invalid host result error=%v; want protocol_error", callErr)
				}
			} else if tc.rejected {
				if callErr == nil || errors.As(callErr, &protocolErr) {
					t.Fatalf("explicit rejection error=%v; want unchanged host rejection", callErr)
				}
			} else if callErr != nil {
				t.Fatalf("accepted result error=%v", callErr)
			}
			if raw := host.lastRawParams(t, MethodHostUIPublish); len(raw) == 0 {
				t.Fatal("no publish reached host")
			}
		})
	}
}
