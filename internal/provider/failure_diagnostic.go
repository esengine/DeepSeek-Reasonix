package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// RequestIdentity keeps the stable connection key separate from the
// user-editable label and the selected wire protocol.
type RequestIdentity struct {
	Provider    string
	DisplayName string
	Protocol    string
}

// RequestFailure preserves connection identity for failures that happen before
// an HTTP status exists, while retaining the original error for classification.
type RequestFailure struct {
	Identity  RequestIdentity
	Operation string
	Err       error
}

func (e *RequestFailure) Error() string {
	return fmt.Sprintf("%s: %s: %v", ProviderDisplayLabel(e.Identity.Provider, e.Identity.DisplayName, e.Identity.Protocol), e.Operation, e.Err)
}

func (e *RequestFailure) Unwrap() error { return e.Err }

func ProtocolDisplayName(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai":
		return "Chat Completions"
	case "anthropic":
		return "Anthropic Messages"
	case "responses":
		return "Responses"
	case "dashscope-responses":
		return "DashScope Responses"
	default:
		return strings.TrimSpace(kind)
	}
}

func ProviderDisplayLabel(providerID, displayName, protocol string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.TrimSpace(providerID)
	}
	protocolName := ProtocolDisplayName(protocol)
	if name == "" {
		return protocolName
	}
	if protocolName == "" {
		return name
	}
	return name + " · " + protocolName
}

// FailureDiagnostic contains safe classification only, never response bodies.
type FailureDiagnostic struct {
	Kind                string `json:"kind"`
	Status              int    `json:"status,omitempty"`
	TraceID             string `json:"traceId,omitempty"`
	ProviderID          string `json:"providerId,omitempty"`
	ProviderDisplayName string `json:"providerDisplayName,omitempty"`
	Protocol            string `json:"protocol,omitempty"`
	RequestPath         string `json:"requestPath,omitempty"`
}

// FailureDiagnosticDetail renders the safe operator fields shared by live and
// persisted failure notices. It intentionally excludes display identity and
// any request query or credentials.
func FailureDiagnosticDetail(d *FailureDiagnostic) string {
	if d == nil {
		return ""
	}
	detail := ""
	if d.ProviderID != "" {
		detail = "Connection ID: " + d.ProviderID
	}
	if d.RequestPath != "" {
		if detail != "" {
			detail += "\n"
		}
		detail += "Request path: " + d.RequestPath
	}
	return detail
}

func DiagnoseFailure(err error) *FailureDiagnostic {
	if err == nil {
		return nil
	}
	d := &FailureDiagnostic{Kind: "unknown"}
	var request *RequestFailure
	if errors.As(err, &request) {
		d.ProviderID = request.Identity.Provider
		d.ProviderDisplayName = request.Identity.DisplayName
		d.Protocol = request.Identity.Protocol
	}
	var quota *QuotaError
	if errors.As(err, &quota) {
		d.ProviderID = quota.Provider
		d.ProviderDisplayName = quota.ProviderDisplayName
		d.Protocol = quota.Protocol
	}
	var api *APIError
	if errors.As(err, &api) {
		d.Status = api.Status
		d.ProviderID = api.Provider
		d.ProviderDisplayName = api.ProviderDisplayName
		d.Protocol = api.Protocol
		if len(api.RequestPath) <= 512 && strings.HasPrefix(api.RequestPath, "/") {
			d.RequestPath = api.RequestPath
		}
		// Trace identifiers are opaque tokens, not arbitrary header text.
		if len(api.TraceID) <= 128 && strings.IndexFunc(api.TraceID, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r))
		}) < 0 {
			d.TraceID = api.TraceID
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		d.Kind = "cancelled"
	case AsRecoveryWaitExhausted(err) != nil:
		d.Kind = "recovery_wait_exhausted"
	case AsQuotaError(err) != nil:
		d.Kind = "quota"
		d.Status = AsQuotaError(err).Status
	case errors.As(err, new(*AuthError)):
		d.Kind = "auth"
		var auth *AuthError
		if errors.As(err, &auth) {
			d.Status = auth.Status
			d.ProviderID = auth.Provider
			d.ProviderDisplayName = auth.ProviderDisplayName
			d.Protocol = auth.Protocol
		}
	case AsContextLimitError(err) != nil || AsOutputLimitError(err) != nil:
		d.Kind = "limit"
	case AsReasoningReplayError(err) != nil:
		d.Kind = "protocol"
	case IsOpaqueBadRequest(err):
		d.Kind = "upstream_reason_missing"
	case ClassifyRecovery(err).Retryable:
		d.Kind = "temporary"
	case api != nil && api.Status >= 400 && api.Status < 500:
		d.Kind = "request"
	}
	return d
}

// IsOpaqueBadRequest deliberately recognizes only empty/model-only 400 bodies.
// An unknown structured error may carry a useful reason and must not be guessed.
func IsOpaqueBadRequest(err error) bool {
	var api *APIError
	if !errors.As(err, &api) || api.Status != 400 {
		return false
	}
	if strings.TrimSpace(api.Body) == "" {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(api.Body), &object) != nil || object == nil {
		return false
	}
	for key := range object {
		if key != "model" {
			return false
		}
	}
	return true
}
