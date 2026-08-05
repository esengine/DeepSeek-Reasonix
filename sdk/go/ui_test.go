package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// uiCall runs fn inside an interceptor ctx (which carries the host
// connection) against a fake host, returning the fake host for frame
// assertions.
func uiCall(t *testing.T, fn func(ctx context.Context) error) (*fakeHost, error) {
	t.Helper()
	var callErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			callErr = fn(ctx)
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostUIPublish, func(json.RawMessage) (any, *hostError) {
		return UIPublishResult{Accepted: true}, nil
	})
	host.onRequest(MethodHostUIRequest, func(json.RawMessage) (any, *hostError) {
		return UIRequestResult{Cancelled: false, Values: map[string]any{"value": true}}, nil
	})
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	return host, callErr
}

// lastRawParams decodes the most recent host request params of one method.
func lastRawParams(t *testing.T, host *fakeHost, method string) json.RawMessage {
	t.Helper()
	return host.lastRawParams(t, method)
}

// TestHostUIPublishStatusGolden pins the exact wire field names of a status
// publish against the canonical schema.
func TestHostUIPublishStatusGolden(t *testing.T) {
	progress := 0.5
	host, err := uiCall(t, func(ctx context.Context) error {
		ui := HostUI{}
		return ui.PublishStatus(ctx, "sess-1", 7, "status-1", UIStatusPayload{
			Label: "Indexing", Detail: "3/6", Severity: UISeverityWarn, Progress: &progress,
		})
	})
	if err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}
	raw := lastRawParams(t, host, MethodHostUIPublish)
	var golden map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("params not an object: %v", err)
	}
	assertJSONFields(t, golden, map[string]any{
		"surfaceId":  "status-1",
		"sessionId":  "sess-1",
		"generation": float64(7),
		"kind":       "status",
	})
	payload, ok := golden["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %v", golden["payload"])
	}
	assertJSONFields(t, payload, map[string]any{
		"label": "Indexing", "detail": "3/6", "severity": "warn", "progress": 0.5,
	})
}

// TestHostUIPublishFormGolden pins the form surface shape.
func TestHostUIPublishFormGolden(t *testing.T) {
	host, err := uiCall(t, func(ctx context.Context) error {
		ui := HostUI{}
		return ui.PublishForm(ctx, "sess-1", 7, "form-1", UIFormPayload{
			Title:   "Configure",
			Message: "Pick values",
			Fields: []UIFormField{
				{Key: "name", Label: "Name", Kind: UIFieldInput, Default: "reasonix", Required: true},
				{Key: "level", Label: "Level", Kind: UIFieldSelect, Options: []string{"low", "high"}},
			},
		})
	})
	if err != nil {
		t.Fatalf("PublishForm: %v", err)
	}
	raw := lastRawParams(t, host, MethodHostUIPublish)
	var doc struct {
		Kind    string `json:"kind"`
		Payload struct {
			Title   string `json:"title"`
			Message string `json:"message"`
			Fields  []struct {
				Key      string   `json:"key"`
				Label    string   `json:"label"`
				Kind     string   `json:"kind"`
				Options  []string `json:"options,omitempty"`
				Default  any      `json:"default,omitempty"`
				Required bool     `json:"required,omitempty"`
			} `json:"fields"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Kind != "form" || doc.Payload.Title != "Configure" || len(doc.Payload.Fields) != 2 {
		t.Fatalf("form doc = %+v", doc)
	}
	name := doc.Payload.Fields[0]
	if name.Key != "name" || name.Kind != "input" || name.Default != "reasonix" || !name.Required {
		t.Fatalf("field 0 = %+v", name)
	}
	level := doc.Payload.Fields[1]
	if level.Kind != "select" || len(level.Options) != 2 || level.Options[1] != "high" {
		t.Fatalf("field 1 = %+v", level)
	}
}

// TestHostUIRequestConfirmGolden pins the confirm prompt shape and answer
// mapping.
func TestHostUIRequestConfirmGolden(t *testing.T) {
	var answer bool
	host, err := uiCall(t, func(ctx context.Context) error {
		ui := HostUI{}
		var callErr error
		answer, callErr = ui.RequestConfirm(ctx, "sess-1", 7, "confirm-1", "Delete everything?")
		return callErr
	})
	if err != nil {
		t.Fatalf("RequestConfirm: %v", err)
	}
	if !answer {
		t.Fatal("confirm answer = false, want true from the scripted host")
	}
	raw := lastRawParams(t, host, MethodHostUIRequest)
	var doc struct {
		SurfaceID  string `json:"surfaceId"`
		SessionID  string `json:"sessionId"`
		Generation uint64 `json:"generation"`
		Kind       string `json:"kind"`
		Payload    struct {
			Message string `json:"message"`
			Fields  []struct {
				Key   string `json:"key"`
				Label string `json:"label"`
				Kind  string `json:"kind"`
			} `json:"fields"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Kind != "confirm" || doc.SurfaceID != "confirm-1" || doc.Generation != 7 {
		t.Fatalf("request doc = %+v", doc)
	}
	if len(doc.Payload.Fields) != 1 || doc.Payload.Fields[0].Key != "value" || doc.Payload.Fields[0].Kind != "confirm" {
		t.Fatalf("confirm fields = %+v", doc.Payload.Fields)
	}
}

// TestHostUIRequestCancelled maps dismissal to ErrUICancelled.
func TestHostUIRequestCancelled(t *testing.T) {
	var callErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			ui := HostUI{}
			_, callErr = ui.RequestConfirm(ctx, "sess-1", 7, "c", "sure?")
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostUIRequest, func(json.RawMessage) (any, *hostError) {
		return UIRequestResult{Cancelled: true}, nil
	})
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if !errors.Is(callErr, ErrUICancelled) {
		t.Fatalf("callErr = %v, want ErrUICancelled", callErr)
	}
}

// TestHostUIRequestMultiSelect decodes a multi-answer from the wire's []any.
func TestHostUIRequestMultiSelect(t *testing.T) {
	var picked []string
	var callErr error
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			ui := HostUI{}
			picked, callErr = ui.RequestMultiSelect(ctx, "sess-1", 7, "ms", MultiSelectPrompt{
				Message: "Pick", Options: []string{"a", "b", "c"},
			})
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.onRequest(MethodHostUIRequest, func(json.RawMessage) (any, *hostError) {
		return UIRequestResult{Cancelled: false, Values: map[string]any{"value": []any{"a", "c"}}}, nil
	})
	host.handshake(t)
	host.request(MethodExtensionIntercept, InterceptParams{
		Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`),
	})
	if callErr != nil {
		t.Fatalf("RequestMultiSelect: %v", callErr)
	}
	if len(picked) != 2 || picked[0] != "a" || picked[1] != "c" {
		t.Fatalf("picked = %v", picked)
	}
}

// TestHostUIValidation rejects invalid payloads before they hit the wire.
func TestHostUIValidation(t *testing.T) {
	ui := HostUI{}
	ctx := context.Background()
	cases := []error{
		ui.PublishStatus(ctx, "s", 1, "x", UIStatusPayload{}),
		ui.PublishStatus(ctx, "s", 1, "x", UIStatusPayload{Label: "l", Severity: "fatal"}),
		ui.PublishNotification(ctx, "s", 1, "x", UINotificationPayload{}),
		ui.PublishForm(ctx, "s", 1, "x", UIFormPayload{}),
		ui.PublishForm(ctx, "s", 1, "x", UIFormPayload{Fields: []UIFormField{{Key: "k", Kind: "textarea"}}}),
		ui.PublishCard(ctx, "s", 1, "x", UICardPayload{Fields: []UIKeyValue{{Value: "v"}}}),
	}
	for i, err := range cases {
		if err == nil {
			t.Fatalf("case %d: expected a validation error", i)
		}
		if errors.Is(err, ErrNoConnection) {
			t.Fatalf("case %d: validation did not run before the connection check", i)
		}
	}
	if _, err := ui.RequestSelect(ctx, "s", 1, "x", SelectPrompt{}); err == nil {
		t.Fatal("select without options: expected a validation error")
	}
}

// assertJSONFields checks want's key/value pairs against got.
func assertJSONFields(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("field %q = %v, want %v (doc %v)", key, got[key], value, got)
		}
	}
}
