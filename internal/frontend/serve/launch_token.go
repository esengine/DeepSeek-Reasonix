package serve

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// codeLaunchTokenRequired refuses a state-changing request that carries no
// launch token while serve authentication is turned off.
const codeLaunchTokenRequired = "auth.launch_token_required"

type hostGatedKey struct{}

// withHostGated marks a request that already proved the host's launch
// credential at a gate in front of this package's own.
func withHostGated(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), hostGatedKey{}, true))
}

// provenOperator reports a request some boundary already authenticated: the
// in-process transport, the loopback gate, or a paired device.
func provenOperator(r *http.Request) bool {
	if fromInProcess(r) {
		return true
	}
	if v, _ := r.Context().Value(hostGatedKey{}).(bool); v {
		return true
	}
	id, ok := DeviceOf(r.Context())
	return ok && id != ""
}

// presentsLaunchToken reports whether r carries this launch's token, as the
// cookie /auth/token issues or as an Authorization bearer.
func (ag *authGate) presentsLaunchToken(r *http.Request) bool {
	if ag.token == "" {
		return false
	}
	if c, err := r.Cookie(cookieToken); err == nil && launchTokenEqual(c.Value, ag.token) {
		return true
	}
	scheme, value, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	return ok && strings.EqualFold(scheme, "Bearer") && launchTokenEqual(value, ag.token)
}

func launchTokenEqual(got, want string) bool {
	got = strings.TrimSpace(got)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// mutationGate holds an auth-disabled serve's mutations to the launch token.
// Approvals require the launch token even then: reaching the listener does not
// make a caller the operator, and any mutation can widen what the agent may do,
// so the method decides rather than a list of routes.
func (ag *authGate) mutationGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ag.mode == authNone && !safeMethod(r.Method) && !provenOperator(r) && !ag.presentsLaunchToken(r) {
			refuse(w, http.StatusForbidden, codeLaunchTokenRequired, "approvals require the launch token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
