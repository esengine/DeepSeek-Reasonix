package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const validSessionEnvelope = `"requestId":"request-1","expectedHostEpoch":"host-1","target":{"workspaceId":"workspace-1","sessionId":"session-1"},"expectedRuntimeEpoch":"runtime-1"`

func TestStrictParamsRejectUnknownMissingNullEnumAndLimit(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		raw  string
	}{
		{"unknown", typeOf[PromptAnswerParams](), `{` + validSessionEnvelope + `,"promptId":"prompt-1","answers":[],"extra":true}`},
		{"missing", typeOf[PromptAnswerParams](), `{` + validSessionEnvelope + `,"promptId":"prompt-1"}`},
		{"required null slice", typeOf[PromptAnswerParams](), `{` + validSessionEnvelope + `,"promptId":"prompt-1","answers":null}`},
		{"optional null", typeOf[WorkspaceListParams](), `{"expectedHostEpoch":"host-1","limit":null}`},
		{"invalid enum", typeOf[PromptApproveParams](), `{` + validSessionEnvelope + `,"promptId":"prompt-1","decision":"always"}`},
		{"limit", typeOf[WorkspaceListParams](), `{"expectedHostEpoch":"host-1","limit":1001}`},
		{"nested null", typeOf[SessionContextParams](), `{"expectedHostEpoch":"host-1","target":null,"expectedRuntimeEpoch":"runtime-1"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeAndValidate(json.RawMessage(test.raw), test.typ); err == nil {
				t.Fatalf("invalid params accepted: %s", test.raw)
			}
		})
	}
}

func TestStrictParamsAcceptEmptyAskAnswersAndEmptyCreateProfile(t *testing.T) {
	answer := `{` + validSessionEnvelope + `,"promptId":"prompt-1","answers":[]}`
	if _, err := decodeAndValidate(json.RawMessage(answer), typeOf[PromptAnswerParams]()); err != nil {
		t.Fatalf("empty Ask answers rejected: %v", err)
	}
	create := `{"requestId":"request-1","expectedHostEpoch":"host-1","workspaceId":"workspace-1","additionalDirectoryRefs":[],"topic":{"kind":"new"},"profile":{}}`
	if _, err := decodeAndValidate(json.RawMessage(create), typeOf[SessionCreateParams]()); err != nil {
		t.Fatalf("Host-default Session profile rejected: %v", err)
	}
}

func TestNestedBusinessValidation(t *testing.T) {
	emptyPatch := `{` + validSessionEnvelope + `,"patch":{}}`
	if _, err := decodeAndValidate(json.RawMessage(emptyPatch), typeOf[SessionProfileSetParams]()); err == nil {
		t.Fatal("empty profile patch accepted")
	}
	badPath := `{"expectedHostEpoch":"host-1","target":{"workspaceId":"workspace-1","sessionId":"session-1"},"expectedRuntimeEpoch":"runtime-1","path":"../secret"}`
	if _, err := decodeAndValidate(json.RawMessage(badPath), typeOf[FilePreviewParams]()); err == nil {
		t.Fatal("workspace-escaping path accepted")
	}
	badPointer := ExternalizedField{JSONPointer: "/tool/~2output", ContentRef: "ref-1", SHA256: strings.Repeat("a", 64)}
	if err := validateDecoded(badPointer); err == nil {
		t.Fatal("invalid RFC 6901 escape accepted")
	}
}

func TestExternalizableRequiredStringMayBeNullOnRawWire(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":null}`)
	if err := validateRequiredJSON(raw, typeOf[HistoryMessage](), "message"); err != nil {
		t.Fatalf("externalized null placeholder rejected: %v", err)
	}
}
