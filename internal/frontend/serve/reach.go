package serve

import (
	"context"
	"net/http"

	"reasonix/internal/contract/provider"
)

// codeDeviceHostOnly refuses a paired device a capability that belongs to the
// window: the device holds what a networked serve client holds, and no more.
// It rides a 404, which a frontend already reads as a kernel with no window.
const codeDeviceHostOnly = "device.host_only"

type deviceReachKey struct{}

// deviceReach is who a device-gated request is: the pairing, and the number
// the window listed it under when the request arrived.
type deviceReach struct {
	id      string
	ordinal int
}

// withDeviceReach marks a request as having arrived through a device gate. Only
// that gate sets it, so a request without the mark reached the kernel through
// the host's own boundary. An unpaired device loading the page has no id.
func withDeviceReach(ctx context.Context, deviceID string, ordinal int) context.Context {
	return context.WithValue(ctx, deviceReachKey{}, deviceReach{id: deviceID, ordinal: ordinal})
}

// DeviceOf names the paired device a request came from, if it came from one.
func DeviceOf(ctx context.Context) (string, bool) {
	reach, ok := ctx.Value(deviceReachKey{}).(deviceReach)
	return reach.id, ok
}

// viaOf is what a request's input is attributed to: the paired device that
// sent it, or nil for the window and a networked serve's own clients.
func viaOf(r *http.Request) *provider.Via {
	reach, ok := r.Context().Value(deviceReachKey{}).(deviceReach)
	if !ok || reach.id == "" {
		return nil
	}
	return &provider.Via{Device: reach.id, Ordinal: reach.ordinal}
}

// hostReach reports whether r carries the authority of the host that opened
// this listener rather than a device paired with it.
func hostReach(r *http.Request) bool {
	_, device := DeviceOf(r.Context())
	return !device
}

// at is what the host opened, as seen by r. A grant is a window's decision
// about its own client, so a device reaching the same kernel holds none of it.
func (g hostGrants) at(r *http.Request) hostGrants {
	if !hostReach(r) {
		return hostGrants{}
	}
	return g
}

// hostOnly serves the routes a window registers for itself and refuses them to
// a paired device; everything else goes to rest.
func hostOnly(host *http.ServeMux, rest http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := host.Handler(r); pattern == "" {
			rest.ServeHTTP(w, r)
			return
		}
		if !hostReach(r) {
			refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
			return
		}
		host.ServeHTTP(w, r)
	})
}
