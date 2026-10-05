package delegation

import (
	"errors"
	"reasonix/internal/runtime/agent"

	"reasonix/internal/contract/provider"
)

// fleetFailureClass is the identity a failed or skipped item carries out to the
// caller. It feeds one decision — re-issue this graph with the finished nodes
// adopted, or change something first — and only the producer knows a rate limit
// from a rejected request, so that decision reads a code and never the message.
type fleetFailureClass string

const (
	// fleetFailureUnclassified is an error its producer left unnamed; reported
	// as such, because guessing from message text is worse than admitting it.
	fleetFailureUnclassified fleetFailureClass = ""
	// fleetFailureProviderTransient is a status a later attempt can plausibly
	// clear, or a stream that broke. The provider's own backoff already gave up.
	fleetFailureProviderTransient fleetFailureClass = "provider.transient"
	// fleetFailureProviderRejected is a status no retry fixes, so re-issuing the
	// same graph buys nothing until the request or the config changes.
	fleetFailureProviderRejected fleetFailureClass = "provider.rejected"
	// fleetFailureContextExhausted is an input that outgrew its window with no
	// compaction left to recover it: a smaller task, not another attempt.
	fleetFailureContextExhausted fleetFailureClass = "context.exhausted"
)

// classifyFleetFailure reads the identity an error already carries and never its
// text, so an error that merely mentions a rate limit stays unclassified. Order
// matters: a context overflow is an APIError too, and the narrower answer is the
// one a caller can act on.
func classifyFleetFailure(err error) fleetFailureClass {
	switch {
	case err == nil:
		return fleetFailureUnclassified
	case errors.Is(err, agent.ErrCompactionRequired), provider.IsContextOverflow(err):
		return fleetFailureContextExhausted
	case provider.IsStreamInterrupted(err):
		return fleetFailureProviderTransient
	}
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) {
		return fleetFailureUnclassified
	}
	if provider.RetryableStatus(apiErr.Status) {
		return fleetFailureProviderTransient
	}
	return fleetFailureProviderRejected
}

// fleetStatusLine puts the identity beside the status, so the caller reads a
// code where it used to have to read the message underneath.
func fleetStatusLine(status string, class fleetFailureClass) string {
	if class == fleetFailureUnclassified {
		return "status: " + status + "\n"
	}
	return "status: " + status + " reason=" + string(class) + "\n"
}
