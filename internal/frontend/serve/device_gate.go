package serve

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

const (
	// DeviceCookie carries a paired device's credential.
	DeviceCookie = "reasonix_device"
	// PairPath is where a device trades a pairing code for its credential.
	PairPath = "/pair"
	// DevicePath is where a paired device asks what it is and what it reaches.
	DevicePath = "/device"
	// DeviceLeavePath is where a device unpairs itself.
	DeviceLeavePath = "/device/leave"

	codeDeviceHost         = "device.host_rejected"
	codeDeviceOrigin       = "device.origin_rejected"
	codeDeviceUnauthorized = "device.unauthorized"
	codeDevicePairing      = "device.pairing_invalid"
	codeDeviceMisconfig    = "device.misconfigured"
	codeNotADevice         = "device.not_a_device"

	deviceCookieMaxAge = 30 * 24 * 60 * 60
)

// DeviceGateOptions is the policy of one listener devices reach.
type DeviceGateOptions struct {
	Registry *DeviceRegistry
	// Origin is what this listener answers for, scheme and authority only:
	// http://192.168.1.20:41234. Requests addressed to any other name are
	// refused, which is what stops a rebinding page from borrowing the socket.
	Origin string
	// Page is the built frontend. Its files are public here, because a device
	// that has not paired yet needs the page to pair from.
	Page fs.FS
	// Machine names the computer the device reaches, for the device to show.
	Machine string
}

// DeviceSelf is a paired device's answer about itself: which of the host's
// devices it is, by the ordinal the host lists it under, and which machine
// it is driving.
type DeviceSelf struct {
	ID      string `json:"id"`
	Ordinal int    `json:"ordinal"`
	Machine string `json:"machine"`
}

// NewDeviceGate guards a kernel reached by paired devices. Every request must
// be addressed to Origin; a state change must name it as its origin; and past
// the page and PairPath, a request must carry a credential the registry knows.
// What passes is marked as device reach, so the window's own grants stay shut.
func NewDeviceGate(next http.Handler, opts DeviceGateOptions) http.Handler {
	origin, authority, secure, ok := deviceOrigin(opts.Origin)
	if !ok || opts.Registry == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			refuse(w, http.StatusInternalServerError, codeDeviceMisconfig, "this gate was built without an origin and a registry", nil)
		})
	}
	reg := opts.Registry
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, authority) {
			refuse(w, http.StatusForbidden, codeDeviceHost, "this listener does not answer for that host", nil)
			return
		}
		if !loopbackOriginAllowed(r, origin) {
			refuse(w, http.StatusForbidden, codeDeviceOrigin, "that origin may not reach this listener", nil)
			return
		}
		if r.URL.Path == PairPath {
			pairDevice(w, r, reg, secure)
			return
		}
		if c, err := r.Cookie(DeviceCookie); err == nil {
			if id, ok := reg.Authenticate(c.Value); ok {
				serveDevice(w, r, next, reg, id, opts.Machine, secure)
				return
			}
		}
		if devicePublicPath(r, opts.Page) {
			// Still device reach: the shell a stranger loads must not come back
			// holding anything the window's grants would add to it.
			next.ServeHTTP(w, r.WithContext(withDeviceReach(r.Context(), "", 0)))
			return
		}
		refuse(w, http.StatusUnauthorized, codeDeviceUnauthorized, "this device is not paired", nil)
	})
}

// serveDevice answers a paired device: its own two routes here, everything
// else through next under device reach. An event stream runs under a context
// unpairing cancels, so a device dropped mid-stream stops receiving at once.
func serveDevice(w http.ResponseWriter, r *http.Request, next http.Handler, reg *DeviceRegistry, id, machine string, secure bool) {
	switch {
	case r.URL.Path == DevicePath && r.Method == http.MethodGet:
		w.Header().Set("Cache-Control", "no-store")
		_, ordinal, _ := reg.Self(id)
		writeJSON(w, DeviceSelf{ID: id, Ordinal: ordinal, Machine: machine})
		return
	case r.URL.Path == DeviceLeavePath && r.Method == http.MethodPost:
		reg.Revoke(id)
		// codeql[go/cookie-secure-not-set] Secure follows the origin's scheme; a LAN listener is plain HTTP.
		http.SetCookie(w, &http.Cookie{Name: DeviceCookie, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, ordinal, _ := reg.Self(id)
	ctx := withDeviceReach(r.Context(), id, ordinal)
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		var release func()
		ctx, release = reg.holdStream(ctx, id)
		defer release()
	}
	next.ServeHTTP(w, r.WithContext(ctx))
}

// notADevice answers the device routes for whoever reached the kernel through
// the host's own boundary: the window, or a browser on a networked serve. A
// page reads the 404 as "this is not a paired device".
func notADevice(w http.ResponseWriter, _ *http.Request) {
	refuse(w, http.StatusNotFound, codeNotADevice, "this client is not a paired device", nil)
}

// devicePublicPath is what an unpaired device may load: the page's own files
// and the shell entry points it routes itself. Nothing that answers from the
// kernel's state.
func devicePublicPath(r *http.Request, page fs.FS) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if tokenBootstrapPublicPath(r) {
		return true
	}
	if page == nil {
		return false
	}
	name := strings.Trim(r.URL.Path, "/")
	if name == "" || !fs.ValidPath(name) {
		return false
	}
	st, err := fs.Stat(page, name)
	return err == nil && !st.IsDir()
}

// pairDevice spends a pairing code. The code rides a JSON body rather than the
// URL, so it never reaches a request line, a log or a referrer.
func pairDevice(w http.ResponseWriter, r *http.Request, reg *DeviceRegistry, secure bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		refuse(w, http.StatusMethodNotAllowed, "request.method_not_allowed", "that method is not accepted here", nil)
		return
	}
	if !jsonContentType(r) {
		refuse(w, http.StatusUnsupportedMediaType, "request.bad_content_type", "the body must be application/json", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var body struct {
		Code string `json:"code"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		badBody(w)
		return
	}
	credential, view, err := reg.Redeem(body.Code, r.UserAgent())
	if err != nil {
		refuse(w, http.StatusForbidden, codeDevicePairing, "that pairing code is not valid; show a new one and scan again", nil)
		return
	}
	// codeql[go/cookie-secure-not-set] Secure follows the origin's scheme; a LAN listener is plain HTTP.
	http.SetCookie(w, &http.Cookie{
		Name:     DeviceCookie,
		Value:    credential,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   deviceCookieMaxAge,
	})
	writeJSON(w, view)
}

func jsonContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "application/json")
}

// deviceOrigin reads an origin a device gate may answer for: http or https,
// an authority with a port, and nothing after it.
func deviceOrigin(raw string) (origin, authority string, secure, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Port() == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return "", "", false, false
	}
	return u.Scheme + "://" + u.Host, u.Host, u.Scheme == "https", true
}
