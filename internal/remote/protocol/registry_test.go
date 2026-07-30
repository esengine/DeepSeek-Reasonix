package protocol

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestFrozenRegistryExactSurface(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(`
remote/initialize remote/ping remote/detach host/capabilities host/configSummary
workspace/browse workspace/open workspace/list workspace/close workspace/changes workspace/changeDetail
catalog/workspace catalog/session topic/list topic/create topic/rename topic/delete topic/trash
session/list session/create session/rename session/close session/trashList session/trash session/restore session/purge
session/subscribe session/unsubscribe session/history session/content session/event session/resync_required catalog/changed
session/submit turn/steer turn/cancel prompt/approve prompt/answer shell/run operation/cancel
session/new session/clear session/fork session/rewind session/compact session/summarize session/profile/set
session/goal/set session/goal/resume session/goal/clear session/context session/balance job/list job/cancel
composer/slashArgs composer/history file/list file/search file/preview git/history git/commitDetail
memory/get memory/suggestions memory/remember memory/forget memory/document/save memory/suggestion/accept
skill/suggestion/accept research/status research/list research/findings research/evidence/record
broker/catalog broker/stream/open broker/stream/cancel broker/stream/chunk broker/stream/end broker/catalog-changed`)
	sort.Strings(want)
	gotSpecs := Registry()
	got := make([]string, len(gotSpecs))
	clientReq, hostNotif, hostReq, clientNotif := 0, 0, 0, 0
	for i, spec := range gotSpecs {
		got[i] = string(spec.Name)
		switch spec.Direction {
		case DirectionClientRequest:
			clientReq++
		case DirectionHostNotification:
			hostNotif++
		case DirectionHostRequest:
			hostReq++
		case DirectionClientNotification:
			clientNotif++
		}
		assertNoDynamicBusinessType(t, spec.ParamsType, map[reflect.Type]bool{})
		assertNoDynamicBusinessType(t, spec.ResultType, map[reflect.Type]bool{})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method surface mismatch\n got: %v\nwant: %v", got, want)
	}
	if clientReq != 69 || hostNotif != 3 || hostReq != 3 || clientNotif != 3 {
		t.Fatalf("directions = clientReq=%d hostNotif=%d hostReq=%d clientNotif=%d", clientReq, hostNotif, hostReq, clientNotif)
	}
}

func TestDecodeRequestParamsUsesRegistryStrictDTO(t *testing.T) {
	raw, err := json.Marshal(validInitializeParams(t))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequestParams(MethodRemoteInitialize, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(InitializeParams); !ok {
		t.Fatalf("decoded initialize type = %T", decoded)
	}
	withUnknown := append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
	if _, err := DecodeRequestParams(MethodRemoteInitialize, withUnknown); err == nil {
		t.Fatal("registry decoder accepted unknown initialize field")
	}
	if _, err := DecodeRequestParams(MethodSessionEvent, json.RawMessage(`{}`)); err == nil {
		t.Fatal("registry decoder accepted a Host notification as a client request")
	}
	if _, err := DecodeRequestParams(Method("unknown/method"), json.RawMessage(`{}`)); err == nil {
		t.Fatal("registry decoder accepted an unregistered method")
	}
}

func TestDecodeResultAndNotificationUseFrozenStrictDTOs(t *testing.T) {
	result, err := DecodeResult(MethodRemotePing, json.RawMessage(`{"hostEpoch":"host-1","leaseTtlMs":30000}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.(PingResult); !ok {
		t.Fatalf("decoded ping result type = %T", result)
	}
	if _, err := DecodeResult(MethodRemotePing, json.RawMessage(`{"hostEpoch":"host-1","leaseTtlMs":30000,"unknown":true}`)); err == nil {
		t.Fatal("result decoder accepted an unknown field")
	}
	if _, err := DecodeResult(MethodSessionEvent, json.RawMessage(`{}`)); err == nil {
		t.Fatal("result decoder accepted a Host notification method")
	}

	notification, err := DecodeNotificationParams(MethodCatalogChanged, json.RawMessage(
		`{"hostEpoch":"host-1","revision":"revision-1","scope":"host","kinds":["sessions"]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := notification.(CatalogChanged); !ok {
		t.Fatalf("decoded catalog notification type = %T", notification)
	}
	if _, err := DecodeNotificationParams(MethodCatalogChanged, json.RawMessage(
		`{"hostEpoch":"host-1","revision":"revision-1","scope":"host","kinds":["sessions"],"unknown":true}`,
	)); err == nil {
		t.Fatal("notification decoder accepted an unknown field")
	}
	if _, err := DecodeNotificationParams(MethodRemotePing, json.RawMessage(`{}`)); err == nil {
		t.Fatal("notification decoder accepted a client request method")
	}
}

func TestDecodeBrokerResultUsesHostRequestDirectionAndStrictDTO(t *testing.T) {
	decoded, err := DecodeBrokerResult(MethodBrokerStreamOpen, json.RawMessage(`{"accepted":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := decoded.(BrokerStreamOpenResult); !ok || !result.Accepted {
		t.Fatalf("decoded Broker result = %#v", decoded)
	}
	if _, err := DecodeBrokerResult(MethodBrokerStreamOpen, json.RawMessage(`{"accepted":true,"unknown":true}`)); err == nil {
		t.Fatal("Broker result decoder accepted unknown field")
	}
	if _, err := DecodeBrokerResult(MethodRemotePing, json.RawMessage(`{}`)); err == nil {
		t.Fatal("Broker result decoder accepted ordinary Remote request")
	}
	if _, err := DecodeBrokerResult(MethodBrokerStreamChunk, json.RawMessage(`{}`)); err == nil {
		t.Fatal("Broker result decoder accepted Broker notification")
	}
	if _, err := DecodeBrokerResult(Method("unknown/method"), json.RawMessage(`{}`)); err == nil {
		t.Fatal("Broker result decoder accepted unregistered method")
	}
}

func assertNoDynamicBusinessType(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if seen[typ] {
		return
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Interface, reflect.Map:
		t.Fatalf("registered DTO contains dynamic type %v", typ)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).PkgPath == "" {
				assertNoDynamicBusinessType(t, typ.Field(i).Type, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		assertNoDynamicBusinessType(t, typ.Elem(), seen)
	}
}

func TestMutationClassesCarryFrozenEnvelopes(t *testing.T) {
	for _, spec := range Registry() {
		if spec.Direction != DirectionClientRequest {
			continue
		}
		fields := flattenedJSONFields(spec.ParamsType)
		require := func(names ...string) {
			for _, name := range names {
				if !fields[name] {
					t.Errorf("%s (%s) is missing %s", spec.Name, spec.Class, name)
				}
			}
		}
		switch spec.Class {
		case ClassHostMutation:
			require("requestId", "expectedHostEpoch")
		case ClassSessionMutation:
			require("requestId", "expectedHostEpoch", "target", "expectedRuntimeEpoch")
		case ClassSessionRecordMutation:
			require("requestId", "expectedHostEpoch", "target")
		}
	}
}

func flattenedJSONFields(typ reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, _, skip := jsonField(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			for nested := range flattenedJSONFields(field.Type) {
				out[nested] = true
			}
			continue
		}
		out[name] = true
	}
	return out
}
