package protocol

import (
	"encoding/json"
	"testing"

	"reasonix/internal/extension/rpcwire"
)

func TestErrorTableCoversRequiredReasons(t *testing.T) {
	required := []ErrorReason{
		ErrProtocolError, ErrUnknownMethod, ErrInvalidParams, ErrFrameTooLarge,
		ErrContentRefExpired, ErrUnsupportedVersion, ErrCapabilityNotDeclared,
		ErrShutdownTimeout, ErrStreamGap, ErrStreamCancelled,
		ErrProviderFailed, ErrProviderInterrupted, ErrInterceptTimeout,
		ErrDependencyUnsatisfied, ErrDependencyCycle, ErrSchemaMismatch,
		ErrActivationFailed, ErrStaleGeneration, ErrCleanupFailed, ErrInternal,
	}
	if len(frozenErrorSpecs) != len(required) {
		t.Fatalf("frozen error table has %d entries, want %d", len(frozenErrorSpecs), len(required))
	}
	for _, reason := range required {
		if _, ok := frozenErrorSpecs[reason]; !ok {
			t.Fatalf("frozen error table is missing %q", reason)
		}
	}
}

func TestErrorContractsAreUniqueSortedAndWellFormed(t *testing.T) {
	contracts := ErrorContracts()
	seen := map[ErrorReason]bool{}
	for i, contract := range contracts {
		if seen[contract.Reason] {
			t.Fatalf("duplicate error reason %q", contract.Reason)
		}
		seen[contract.Reason] = true
		if i > 0 && contracts[i-1].Reason >= contract.Reason {
			t.Fatalf("error contracts not sorted at %q", contract.Reason)
		}
		if contract.Message == "" {
			t.Fatalf("%q has an empty frozen message", contract.Reason)
		}
		switch contract.JSONRPCCode {
		case DomainErrorCode, rpcwire.ErrInvalidRequest, rpcwire.ErrMethodNotFound,
			rpcwire.ErrInvalidParams, rpcwire.ErrInternal:
		default:
			t.Fatalf("%q uses non-contract JSON-RPC code %d", contract.Reason, contract.JSONRPCCode)
		}
		spec := frozenErrorSpecs[contract.Reason]
		if spec.Code != contract.JSONRPCCode || spec.Message != contract.Message || spec.Retryable != contract.Retryable {
			t.Fatalf("%q contract does not match its frozen spec", contract.Reason)
		}
	}
}

func TestProtocolErrorWireShape(t *testing.T) {
	err := MustProtocolError(ErrStreamGap)
	if err.Error() == "" {
		t.Fatal("ProtocolError.Error() is empty")
	}
	rpcErr := err.RPCError()
	if rpcErr.Code != DomainErrorCode {
		t.Fatalf("RPCError code = %d, want %d", rpcErr.Code, DomainErrorCode)
	}
	raw, marshalErr := json.Marshal(rpcErr.Data)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var decoded ProtocolErrorData
	if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if decoded.Reason != ErrStreamGap || !decoded.Retryable {
		t.Fatalf("wire data = %+v, want stream_gap/retryable", decoded)
	}
	if validateErr := decoded.Validate(); validateErr != nil {
		t.Fatalf("wire data failed validation: %v", validateErr)
	}

	// Standard JSON-RPC codes ride on the mapped reasons.
	standard := map[ErrorReason]int{
		ErrProtocolError: rpcwire.ErrInvalidRequest,
		ErrUnknownMethod: rpcwire.ErrMethodNotFound,
		ErrInvalidParams: rpcwire.ErrInvalidParams,
		ErrInternal:      rpcwire.ErrInternal,
	}
	for reason, code := range standard {
		if got := MustProtocolError(reason).RPCError().Code; got != code {
			t.Fatalf("%s RPCError code = %d, want %d", reason, got, code)
		}
	}

	var nilErr *ProtocolError
	if nilErr.Error() != "" || nilErr.RPCError().Code != rpcwire.ErrInternal {
		t.Fatal("nil ProtocolError must degrade to a plain internal error")
	}
}

func TestProtocolErrorDataValidate(t *testing.T) {
	if err := (ProtocolErrorData{Reason: ErrShutdownTimeout, Retryable: true}).Validate(); err != nil {
		t.Fatalf("valid data rejected: %v", err)
	}
	if err := (ProtocolErrorData{Reason: ErrShutdownTimeout, Retryable: false}).Validate(); err == nil {
		t.Fatal("retryable mismatch accepted")
	}
	if err := (ProtocolErrorData{Reason: "bogus"}).Validate(); err == nil {
		t.Fatal("unknown reason accepted")
	}
	if _, err := NewProtocolError("bogus"); err == nil {
		t.Fatal("NewProtocolError accepted an unknown reason")
	}
}
