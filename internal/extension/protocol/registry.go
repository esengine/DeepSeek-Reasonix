package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type Method string

const (
	// Lifecycle (Host → Extension).
	MethodExtensionInitialize       Method = "extension/initialize"
	MethodExtensionInitialized      Method = "extension/initialized"
	MethodExtensionShutdown         Method = "extension/shutdown"
	MethodExtensionIntercept        Method = "extension/intercept"
	MethodExtensionEvent            Method = "extension/event"
	MethodExtensionResourcesChanged Method = "extension/resources/changed"

	// Extension-hosted provider broker. Catalog/open/cancel run Host →
	// Extension; stream chunks and stream end flow back as notifications.
	MethodExtensionProviderCatalog      Method = "extension/provider/catalog"
	MethodExtensionProviderStreamOpen   Method = "extension/provider/stream/open"
	MethodExtensionProviderStreamCancel Method = "extension/provider/stream/cancel"
	MethodExtensionProviderStreamChunk  Method = "extension/provider/stream/chunk"
	MethodExtensionProviderStreamEnd    Method = "extension/provider/stream/end"

	// UI. Action invocations and form submissions run Host → Extension;
	// surfaces and blocking prompts are Extension → Host requests.
	MethodExtensionUIAction Method = "extension/ui/action"
	MethodExtensionUISubmit Method = "extension/ui/submit"
	MethodHostUIPublish     Method = "host/ui/publish"
	MethodHostUIRequest     Method = "host/ui/request"

	// Externalized content reads (Extension → Host).
	MethodHostContentRead Method = "host/content/read"
)

// MethodSpec is one frozen registry entry: the method name, its direction,
// its operation class, and the exact wire DTOs for params and result.
type MethodSpec struct {
	Name       Method
	Direction  Direction
	Class      OperationClass
	ParamsType reflect.Type
	ResultType reflect.Type
}

// Notification reports whether the method carries no response.
func (s MethodSpec) Notification() bool { return s.Direction.IsNotification() }

func hostRequest[P, R any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionHostToExtensionRequest, class, typeOf[P](), typeOf[R]()}
}

func extensionRequest[P, R any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionExtensionToHostRequest, class, typeOf[P](), typeOf[R]()}
}

func hostNotification[P any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionHostToExtensionNotification, class, typeOf[P](), typeOf[NoResult]()}
}

func extensionNotification[P any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionExtensionToHostNotification, class, typeOf[P](), typeOf[NoResult]()}
}

func typeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// frozenRegistry is the Extension Protocol v2 method set. Adding, renaming,
// or redirecting a method is a conscious protocol change: ValidateRegistry
// pins the counts and the generated schema hash changes.
var frozenRegistry = []MethodSpec{
	hostRequest[InitializeParams, InitializeResult](MethodExtensionInitialize, ClassLifecycle),
	hostNotification[InitializedParams](MethodExtensionInitialized, ClassLifecycle),
	hostRequest[ShutdownParams, ShutdownResult](MethodExtensionShutdown, ClassLifecycle),
	hostRequest[InterceptParams, InterceptResult](MethodExtensionIntercept, ClassIntercept),
	hostNotification[EventParams](MethodExtensionEvent, ClassObservation),
	hostNotification[ResourcesChangedParams](MethodExtensionResourcesChanged, ClassObservation),
	hostRequest[ProviderCatalogParams, ProviderCatalogResult](MethodExtensionProviderCatalog, ClassProvider),
	hostRequest[StreamOpenParams, StreamOpenResult](MethodExtensionProviderStreamOpen, ClassProvider),
	hostRequest[StreamCancelParams, StreamCancelResult](MethodExtensionProviderStreamCancel, ClassProvider),
	extensionNotification[StreamChunkParams](MethodExtensionProviderStreamChunk, ClassProvider),
	extensionNotification[StreamEndParams](MethodExtensionProviderStreamEnd, ClassProvider),
	hostRequest[UIActionParams, UIActionResult](MethodExtensionUIAction, ClassUI),
	hostRequest[UISubmitParams, UISubmitResult](MethodExtensionUISubmit, ClassUI),
	extensionRequest[UIPublishParams, UIPublishResult](MethodHostUIPublish, ClassUI),
	extensionRequest[UIRequestParams, UIRequestResult](MethodHostUIRequest, ClassUI),
	extensionRequest[ContentReadParams, ContentReadResult](MethodHostContentRead, ClassContent),
}

var frozenRegistryByName = buildRegistryIndex(frozenRegistry)

func buildRegistryIndex(specs []MethodSpec) map[Method]MethodSpec {
	index := make(map[Method]MethodSpec, len(specs))
	for _, spec := range specs {
		if _, duplicate := index[spec.Name]; duplicate {
			panic("protocol: duplicate method " + string(spec.Name))
		}
		if spec.ParamsType.Kind() != reflect.Struct || spec.ResultType.Kind() != reflect.Struct {
			panic("protocol: method types must be structs: " + string(spec.Name))
		}
		index[spec.Name] = spec
	}
	return index
}

// Registry returns the frozen methods sorted by name.
func Registry() []MethodSpec {
	out := append([]MethodSpec(nil), frozenRegistry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LookupMethod finds one frozen method by name.
func LookupMethod(name Method) (MethodSpec, bool) {
	spec, ok := frozenRegistryByName[name]
	return spec, ok
}

// DecodeHostRequestParams applies the registry's exact DTO and strict decoder
// for a Host → Extension request.
func DecodeHostRequestParams(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionHostToExtensionRequest, true)
}

// DecodeHostRequestResult applies the frozen result DTO for a Host →
// Extension request; the extension side decodes host answers with it.
func DecodeHostRequestResult(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionHostToExtensionRequest, false)
}

// DecodeExtensionRequestParams applies the registry's exact DTO and strict
// decoder for an Extension → Host request.
func DecodeExtensionRequestParams(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionExtensionToHostRequest, true)
}

// DecodeExtensionRequestResult applies the frozen result DTO for an
// Extension → Host request; the host side decodes extension answers with it.
func DecodeExtensionRequestResult(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionExtensionToHostRequest, false)
}

// DecodeHostNotificationParams applies the strict decoder to a Host →
// Extension notification payload.
func DecodeHostNotificationParams(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionHostToExtensionNotification, true)
}

// DecodeExtensionNotificationParams applies the strict decoder to an
// Extension → Host notification payload.
func DecodeExtensionNotificationParams(method Method, raw json.RawMessage) (any, error) {
	return decodeForDirection(method, raw, DirectionExtensionToHostNotification, true)
}

func decodeForDirection(method Method, raw json.RawMessage, direction Direction, params bool) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != direction {
		return nil, fmt.Errorf("protocol: %q is not a %s", method, direction)
	}
	if params {
		return decodeAndValidate(raw, spec.ParamsType)
	}
	if spec.Notification() {
		return nil, fmt.Errorf("protocol: %q is a notification and has no result", method)
	}
	return decodeAndValidate(raw, spec.ResultType)
}

// ValidateRegistry pins the frozen method counts so adding a method is a
// conscious act that must update this contract and regenerate the schema.
func ValidateRegistry() error {
	hostReq, extReq, hostNotif, extNotif := 0, 0, 0, 0
	for _, spec := range frozenRegistry {
		switch spec.Direction {
		case DirectionHostToExtensionRequest:
			hostReq++
		case DirectionExtensionToHostRequest:
			extReq++
		case DirectionHostToExtensionNotification:
			hostNotif++
		case DirectionExtensionToHostNotification:
			extNotif++
		default:
			return fmt.Errorf("method %s has invalid direction %q", spec.Name, spec.Direction)
		}
	}
	// Extension Protocol v2: 8 lifecycle/intercept/provider/UI Host →
	// Extension requests, 3 Extension → Host requests (UI publish/request,
	// content read), 3 Host → Extension notifications, 2 provider stream
	// notifications.
	if len(frozenRegistry) != 16 || hostReq != 8 || extReq != 3 || hostNotif != 3 || extNotif != 2 {
		return fmt.Errorf("registry count = total=%d hostReq=%d extReq=%d hostNotif=%d extNotif=%d, want 16/8/3/3/2",
			len(frozenRegistry), hostReq, extReq, hostNotif, extNotif)
	}
	return nil
}
