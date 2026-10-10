// probe_failure.go — why a probe failed, as an identity rather than a sentence.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"reasonix/internal/model/openai"
	"reasonix/internal/safety/redirectguard"
)

// ProbeReason names why probing failed. Each one sends the user to a different
// fix, and a message cannot carry that: only the probe knows a refused key from
// a path the endpoint does not serve.
type ProbeReason string

const (
	ProbeAddressMissing  ProbeReason = "address_missing"
	ProbeUnauthorized    ProbeReason = "unauthorized"
	ProbePaymentRequired ProbeReason = "payment_required"
	ProbeRateLimited     ProbeReason = "rate_limited"
	ProbePathNotFound    ProbeReason = "path_not_found"
	ProbeNoChatModels    ProbeReason = "no_chat_models"
	ProbeUpstreamError   ProbeReason = "upstream_error"
	ProbeTimeout         ProbeReason = "timeout"
	ProbeUnreachable     ProbeReason = "unreachable"
	ProbeNotCompatible   ProbeReason = "not_compatible"
	ProbeRedirectRefused ProbeReason = "redirect_refused"
)

// ProbeError carries that identity out. Params hold the facts a sentence needs
// — a model count, a status — so the wording stays with whoever has a reader.
type ProbeError struct {
	Reason ProbeReason
	Params map[string]any
	// Status and Body are what the endpoint answered with; zero and empty when
	// it never answered.
	Status int
	Body   string
	detail string
	err    error
}

func (e *ProbeError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("probe: %s: %v", e.Reason, e.err)
	}
	return fmt.Sprintf("probe: %s: %s", e.Reason, e.detail)
}

func (e *ProbeError) Unwrap() error { return e.err }

// probeReasonRank orders diagnoses by how far each narrows the fix. Every shape
// is tried, so several failures arrive together and the vaguest must not win: a
// listing that answered with no chat models settles the question, a 401 says
// more than a 404, and "not compatible" is what is left when nothing spoke.
var probeReasonRank = map[ProbeReason]int{
	ProbeRedirectRefused: -1,
	ProbeNoChatModels:    0,
	ProbePaymentRequired: 1,
	ProbeUnauthorized:    2,
	ProbeRateLimited:     3,
	ProbeUpstreamError:   4,
	ProbePathNotFound:    5,
	ProbeTimeout:         6,
	ProbeUnreachable:     7,
	ProbeNotCompatible:   8,
}

// classifyProbe reads one failed listing attempt as an identity. The status is
// the evidence; a failure that never reached a response is unreachable.
func classifyProbe(err error) *ProbeError {
	var known *ProbeError
	if errors.As(err, &known) {
		return known
	}
	if errors.Is(err, redirectguard.ErrRefused) {
		return &ProbeError{Reason: ProbeRedirectRefused, err: err}
	}
	status, ok := openai.ModelFetchStatus(err)
	if !ok {
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return &ProbeError{Reason: ProbeTimeout, err: err}
		}
		return &ProbeError{Reason: ProbeUnreachable, err: err}
	}
	body, _ := openai.ModelFetchBody(err)
	reason := ProbeNotCompatible
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		reason = ProbeUnauthorized
	case status == http.StatusPaymentRequired:
		reason = ProbePaymentRequired
	case status == http.StatusTooManyRequests:
		reason = ProbeRateLimited
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		reason = ProbePathNotFound
	case status >= 500:
		reason = ProbeUpstreamError
	}
	return &ProbeError{Reason: reason, Params: map[string]any{"status": status}, Status: status, Body: body, err: err}
}

// keepWorseDiagnosis keeps whichever of the two narrows the fix further.
func keepWorseDiagnosis(best, next *ProbeError) *ProbeError {
	if best == nil {
		return next
	}
	if next == nil {
		return best
	}
	if probeReasonRank[next.Reason] < probeReasonRank[best.Reason] {
		return next
	}
	return best
}
