package remotecloud

import (
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"

	"reasonix/internal/platform/account"
)

// Why a Studio is not online through the relay. Empty means online, still
// connecting, or a failure none of these names.
const (
	ReasonSignedOut   = "signed_out"
	ReasonUnreachable = "unreachable"
	ReasonRefused     = "refused"
	ReasonUnavailable = "unavailable"
)

// errRelayUnavailable marks a relay handshake answered with throttling or a
// server failure: the relay is busy or down, not refusing this device.
var errRelayUnavailable = errors.New("remote cloud: relay unavailable")

func handshakeError(response *http.Response, err error) error {
	if response != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError) {
		return fmt.Errorf("%w: %w", errRelayUnavailable, err)
	}
	return err
}

// offlineReason classifies a failed sign-in check or relay connection by the
// error's identity, never its text.
func offlineReason(err error) string {
	var apiErr *account.Error
	var netErr net.Error
	switch {
	case errors.Is(err, account.ErrUnauthorized):
		return ReasonSignedOut
	case errors.Is(err, errRelayUnavailable), errors.As(err, &apiErr) && apiErr.Unavailable():
		return ReasonUnavailable
	case errors.Is(err, websocket.ErrBadHandshake), errors.As(err, &apiErr):
		return ReasonRefused
	case errors.As(err, &netErr):
		return ReasonUnreachable
	}
	return ""
}
