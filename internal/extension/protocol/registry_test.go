package protocol

import (
	"strings"
	"testing"
)

func TestRegistryIsSortedAndPinned(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatalf("ValidateRegistry: %v", err)
	}
	registry := Registry()
	if len(registry) != 16 {
		t.Fatalf("registry has %d methods, want 16", len(registry))
	}
	for i := 1; i < len(registry); i++ {
		if registry[i-1].Name >= registry[i].Name {
			t.Fatalf("registry is not strictly sorted at %q >= %q", registry[i-1].Name, registry[i].Name)
		}
	}
}

func TestRegistryMethodDirections(t *testing.T) {
	want := map[Method]Direction{
		MethodExtensionInitialize:           DirectionHostToExtensionRequest,
		MethodExtensionInitialized:          DirectionHostToExtensionNotification,
		MethodExtensionShutdown:             DirectionHostToExtensionRequest,
		MethodExtensionIntercept:            DirectionHostToExtensionRequest,
		MethodExtensionEvent:                DirectionHostToExtensionNotification,
		MethodExtensionResourcesChanged:     DirectionHostToExtensionNotification,
		MethodExtensionProviderCatalog:      DirectionHostToExtensionRequest,
		MethodExtensionProviderStreamOpen:   DirectionHostToExtensionRequest,
		MethodExtensionProviderStreamCancel: DirectionHostToExtensionRequest,
		MethodExtensionProviderStreamChunk:  DirectionExtensionToHostNotification,
		MethodExtensionProviderStreamEnd:    DirectionExtensionToHostNotification,
		MethodExtensionUIAction:             DirectionHostToExtensionRequest,
		MethodExtensionUISubmit:             DirectionHostToExtensionRequest,
		MethodHostUIPublish:                 DirectionExtensionToHostRequest,
		MethodHostUIRequest:                 DirectionExtensionToHostRequest,
		MethodHostContentRead:               DirectionExtensionToHostRequest,
	}
	if len(want) != 16 {
		t.Fatalf("test pins %d methods, want 16", len(want))
	}
	for method, direction := range want {
		spec, ok := LookupMethod(method)
		if !ok {
			t.Fatalf("LookupMethod(%q) not found", method)
		}
		if spec.Direction != direction {
			t.Fatalf("%s direction = %q, want %q", method, spec.Direction, direction)
		}
		if spec.Notification() != direction.IsNotification() {
			t.Fatalf("%s notification flag disagrees with direction %q", method, direction)
		}
	}
	if _, ok := LookupMethod("extension/bogus"); ok {
		t.Fatal("LookupMethod accepted an unregistered method")
	}
}

func TestRegistryClasses(t *testing.T) {
	want := map[Method]OperationClass{
		MethodExtensionInitialize:           ClassLifecycle,
		MethodExtensionInitialized:          ClassLifecycle,
		MethodExtensionShutdown:             ClassLifecycle,
		MethodExtensionIntercept:            ClassIntercept,
		MethodExtensionEvent:                ClassObservation,
		MethodExtensionResourcesChanged:     ClassObservation,
		MethodExtensionProviderCatalog:      ClassProvider,
		MethodExtensionProviderStreamOpen:   ClassProvider,
		MethodExtensionProviderStreamCancel: ClassProvider,
		MethodExtensionProviderStreamChunk:  ClassProvider,
		MethodExtensionProviderStreamEnd:    ClassProvider,
		MethodExtensionUIAction:             ClassUI,
		MethodExtensionUISubmit:             ClassUI,
		MethodHostUIPublish:                 ClassUI,
		MethodHostUIRequest:                 ClassUI,
		MethodHostContentRead:               ClassContent,
	}
	for method, class := range want {
		spec, _ := LookupMethod(method)
		if spec.Class != class {
			t.Fatalf("%s class = %q, want %q", method, spec.Class, class)
		}
	}
}

func TestDecodeHelpersRejectWrongDirection(t *testing.T) {
	raw := []byte(`{}`)
	if _, err := DecodeHostRequestParams(MethodHostContentRead, raw); err == nil {
		t.Fatal("DecodeHostRequestParams accepted an extension request method")
	}
	if _, err := DecodeExtensionRequestParams(MethodExtensionInitialize, raw); err == nil {
		t.Fatal("DecodeExtensionRequestParams accepted a host request method")
	}
	if _, err := DecodeHostNotificationParams(MethodExtensionEvent, []byte(`{"event":"session.start","payload":{}}`)); err == nil {
		// extension/event IS a host notification; must decode.
	} else {
		t.Fatalf("DecodeHostNotificationParams(extension/event) = %v", err)
	}
	if _, err := DecodeExtensionNotificationParams(MethodExtensionEvent, raw); err == nil {
		t.Fatal("DecodeExtensionNotificationParams accepted a host notification method")
	}
	if _, err := DecodeHostRequestParams("extension/bogus", raw); err == nil {
		t.Fatal("DecodeHostRequestParams accepted an unregistered method")
	}
	if _, err := DecodeHostRequestResult(MethodExtensionEvent, raw); err == nil {
		t.Fatal("DecodeHostRequestResult accepted a notification (no result)")
	}
	if _, err := DecodeExtensionRequestResult(MethodExtensionProviderStreamChunk, raw); err == nil {
		t.Fatal("DecodeExtensionRequestResult accepted a notification method")
	}
}

func TestInterceptEventsFrozen(t *testing.T) {
	events := InterceptEvents()
	if len(events) != 17 {
		t.Fatalf("InterceptEvents has %d entries, want 17", len(events))
	}
	seen := map[string]bool{}
	for i, event := range events {
		if seen[event] {
			t.Fatalf("duplicate intercept event %q", event)
		}
		seen[event] = true
		if i > 0 && events[i-1] >= event {
			t.Fatalf("intercept events not sorted at %q", event)
		}
		if !strings.Contains(event, ".") {
			t.Fatalf("intercept event %q does not follow the <area>.<point> shape", event)
		}
	}
	// Every frozen event must round-trip through the strict enum check.
	for _, event := range events {
		raw := []byte(`{"event":"` + event + `","payload":{}}`)
		if _, err := DecodeHostNotificationParams(MethodExtensionEvent, raw); err != nil {
			t.Fatalf("frozen event %q rejected: %v", event, err)
		}
	}
	raw := []byte(`{"event":"session.bogus","payload":{}}`)
	if _, err := DecodeHostNotificationParams(MethodExtensionEvent, raw); err == nil {
		t.Fatal("unknown intercept event accepted")
	}
}
