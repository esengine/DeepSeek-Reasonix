// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// TODO: Handle TLS proxy

// ProxyConfig defines the config for Proxy middleware.
type ProxyConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Balancer defines a load balancing technique.
	// Required.
	Balancer ProxyBalancer

	// RetryCount defines the number of times a failed proxied request should be retried
	// using the next available ProxyTarget. Defaults to 0, meaning requests are never retried.
	RetryCount int

	// RetryFilter defines a function used to determine if a failed request to a
	// ProxyTarget should be retried. The RetryFilter will only be called when the number
	// of previous retries is less than RetryCount. If the function returns true, the
	// request will be retried. The provided error indicates the reason for the request
	// failure. When the ProxyTarget is unavailable, the error will be an instance of
	// echo.HTTPError with a Code of http.StatusBadGateway. In all other cases, the error
	// will indicate an internal error in the Proxy middleware. When a RetryFilter is not
	// specified, all requests that fail with http.StatusBadGateway will be retried. A custom
	// RetryFilter can be provided to only retry specific requests. Note that RetryFilter is
	// only called when the request to the target fails, or an internal error in the Proxy
	// middleware has occurred. Successful requests that return a non-200 response code cannot
	// be retried.
	RetryFilter func(c echo.Context, e error) bool

	// ErrorHandler defines a function which can be used to return custom errors from
	// the Proxy middleware. ErrorHandler is only invoked when there has been
	// either an internal error in the Proxy middleware or the ProxyTarget is
	// unavailable. Due to the way requests are proxied, ErrorHandler is not invoked
	// when a ProxyTarget returns a non-200 response. In these cases, the response
	// is already written so errors cannot be modified. ErrorHandler is only
	// invoked after all retry attempts have been exhausted.
	ErrorHandler func(c echo.Context, err error) error

	// Rewrite defines URL path rewrite rules. The values captured in asterisk can be
	// retrieved by index e.g. $1, $2 and so on.
	// Examples:
	// "/old":              "/new",
	// "/api/*":            "/$1",
	// "/js/*":             "/public/javascripts/$1",
	// "/users/*/orders/*": "/user/$1/order/$2",
	Rewrite map[string]string

	// RegexRewrite defines rewrite rules using regexp.Rexexp with captures
	// Every capture group in the values can be retrieved by index e.g. $1, $2 and so on.
	// Example:
	// "^/old/[0.9]+/":     "/new",
	// "^/api/.+?/(.*)":    "/v2/$1",
	RegexRewrite map[*regexp.Regexp]string

	// Context key to store selected ProxyTarget into context.
	// Optional. Default value "target".
	ContextKey string

	// To customize the transport to remote.
	// Examples: If custom TLS certificates are required.
	Transport http.RoundTripper

	// ModifyResponse defines function to modify response from ProxyTarget.
	ModifyResponse func(*http.Response) error
}

// ProxyTarget defines the upstream target.
type ProxyTarget struct {
	Name string
	URL  *url.URL
	Meta echo.Map
}

// ProxyBalancer defines an interface to implement a load balancing technique.
type ProxyBalancer interface {
	AddTarget(*ProxyTarget) bool
	RemoveTarget(string) bool
	Next(echo.Context) *ProxyTarget
}

// TargetProvider defines an interface that gives the opportunity for balancer
// to return custom errors when selecting target.
type TargetProvider interface {
	NextTarget(echo.Context) (*ProxyTarget, error)
}

type commonBalancer struct {
	targets []*ProxyTarget
	mutex   sync.Mutex
}

// RandomBalancer implements a random load balancing technique.
type randomBalancer struct {
	commonBalancer
	random *rand.Rand
}

// RoundRobinBalancer implements a round-robin load balancing technique.
type roundRobinBalancer struct {
	commonBalancer
	// tracking the index on `targets` slice for the next `*ProxyTarget` to be used
	i int
}

// DefaultProxyConfig is the default Proxy middleware config.
var DefaultProxyConfig = ProxyConfig{
	Skipper:    DefaultSkipper,
	ContextKey: "target",
}

func proxyRaw(t *ProxyTarget, c echo.Context, config ProxyConfig) http.Handler {
	var dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
	if transport, ok := config.Transport.(*http.Transport); ok {
		if transport.TLSClientConfig != nil {
			d := tls.Dialer{
				Config: transport.TLSClientConfig,
			}
			dialFunc = d.DialContext
		}
	}
	if dialFunc == nil {
		var d net.Dialer
		dialFunc = d.DialContext
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, _, err := c.Response().Hijack()
		if err != nil {
			c.Set("_error", fmt.Errorf("proxy raw, hijack error=%w, url=%s", err, t.URL))
			return
		}
		defer in.Close()
		out, err := dialFunc(c.Request().Context(), "tcp", t.URL.Host)
		if err != nil {
			c.Set("_error", echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("proxy raw, dial error=%v, url=%s", err, t.URL)))
			return
		}
		defer out.Close()

		// Write header
		err = r.Write(out)
		if err != nil {
			c.Set("_error", echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("proxy raw, request header copy error=%v, url=%s", err, t.URL)))
			return
		}

		errCh := make(chan error, 2)
		cp := func(dst io.Writer, src io.Reader) {
			_, copyErr := io.Copy(dst, src)
			errCh <- copyErr
		}

		go cp(out, in)
		go cp(in, out)

		// Wait for BOTH goroutines to complete
		err1 := <-errCh
		err2 := <-errCh

		if err1 != nil && err1 != io.EOF {
			c.Set("_error", fmt.Errorf("proxy raw, copy body error=%w, url=%s", err1, t.URL))
		} else if err2 != nil && err2 != io.EOF {
			c.Set("_error", fmt.Errorf("proxy raw, copy body error=%w, url=%s", err2, t.URL))
		}
	})
}

// NewRandomBalancer returns a random proxy balancer.
func NewRandomBalancer(targets []*ProxyTarget) ProxyBalancer {
	b := randomBalancer{}
	b.targets = targets
	b.random = rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	return &b
}

// NewRoundRobinBalancer returns a round-robin proxy balancer.
func NewRoundRobinBalancer(targets []*ProxyTarget) ProxyBalancer {
	b := roundRobinBalancer{}
	b.targets = targets
	return &b
}

// AddTarget adds an upstream target to the list and returns `true`.
//
// However, if a target with the same name already exists then the operation is aborted returning `false`.
func (b *commonBalancer) AddTarget(target *ProxyTarget) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, t := range b.targets {
		if t.Name == target.Name {
			return false
		}
	}
	b.targets = append(b.targets, target)
	return true
}

// RemoveTarget removes an upstream target from the list by name.
//
// Returns `true` on success, `false` if no target with the name is found.
func (b *commonBalancer) RemoveTarget(name string) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for i, t := range b.targets {
		if t.Name == name {
			b.targets = append(b.targets[:i], b.targets[i+1:]...)
			return true
		}
	}
	return false
}

// Next randomly returns an upstream target.
//
// Note: `nil` is returned in case upstream target list is empty.
func (b *randomBalancer) Next(c echo.Context) *ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.targets) == 0 {
		return nil
	} else if len(b.targets) == 1 {
		return b.targets[0]
	}
	return b.targets[b.random.Intn(len(b.targets))]
}

// Next returns an upstream target using round-robin technique. In the case
// where a previously failed request is being retried, the round-robin
// balancer will attempt to use the next target relative to the original
// request. If the list of targets held by the balancer is modified while a
// failed request is being retried, it is possible that the balancer will
// return the original failed target.
//
// Note: `nil` is returned in case upstream target list is empty.
func (b *roundRobinBalancer) Next(c echo.Context) *ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.targets) == 0 {
		return nil
	} else if len(b.targets) == 1 {
		return b.targets[0]
	}

	var i int
	const lastIdxKey = "_round_robin_last_index"
	// This request is a retry, start from the index of the previous
	// target to ensure we don't attempt to retry the request with
	// the same failed target
	if c.Get(lastIdxKey) != nil {
		i = c.Get(lastIdxKey).(int)
		i++
		if i >= len(b.targets) {
			i = 0
		}
	} else {
		// This is a first time request, use the global index
		if b.i >= len(b.targets) {
			b.i = 0
		}
		i = b.i
		b.i++
	}

	c.Set(lastIdxKey, i)
	return b.targets[i]
}

// Proxy returns a Proxy middleware.
//
// Proxy middleware forwards the request to upstream server using a configured load balancing technique.
func Proxy(balancer ProxyBalancer) echo.MiddlewareFunc {
	c := DefaultProxyConfig
	c.Balancer = balancer
	return ProxyWithConfig(c)
}

// ProxyWithConfig returns a Proxy middleware with config.
// See: `Proxy()`
func ProxyWithConfig(config ProxyConfig) echo.MiddlewareFunc {
	if config.Balancer == nil {
		panic("echo: proxy middleware requires balancer")
	}
	// Defaults
	if config.Skipper == nil {
		config.Skipper = DefaultProxyConfig.Skipper
	}
	if config.RetryFilter == nil {
		config.RetryFilter = func(c echo.Context, e error) bool {
			if httpErr, ok := e.(*echo.HTTPError); ok {
				return httpErr.Code == http.StatusBadGateway
			}
			return false
		}
	}
	if config.ErrorHandler == nil {
		config.ErrorHandler = func(c echo.Context, err error) error {
			return err
		}
	}
	if config.Rewrite != nil {
		if config.RegexRewrite == nil {
			config.RegexRewrite = make(map[*regexp.Regexp]string)
		}
		for k, v := range rewriteRulesRegex(config.Rewrite) {
			config.RegexRewrite[k] = v
		}
	}

	provider, isTargetProvider := config.Balancer.(TargetProvider)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			req := c.Request()
			res := c.Response()
			if err := rewriteURL(config.RegexRewrite, req); err != nil {
				return config.ErrorHandler(c, err)
			}

			// Fix header
			// Basically it's not good practice to unconditionally pass incoming x-real-ip header to upstream.
			// However, for backward compatibility, legacy behavior is preserved unless you configure Echo#IPExtractor.
			if req.Header.Get(echo.HeaderXRealIP) == "" || c.Echo().IPExtractor != nil {
				req.Header.Set(echo.HeaderXRealIP, c.RealIP())
			}
			if req.Header.Get(echo.HeaderXForwardedProto) == "" {
				req.Header.Set(echo.HeaderXForwardedProto, c.Scheme())
			}
			if c.IsWebSocket() && req.Header.Get(echo.HeaderXForwardedFor) == "" { // For HTTP, it is automatically set by Go HTTP reverse proxy.
				req.Header.Set(echo.HeaderXForwardedFor, c.RealIP())
			}

			retries := config.RetryCount
			for {
				var tgt *ProxyTarget
				var err error
				if isTargetProvider {
					tgt, err = provider.NextTarget(c)
					if err != nil {
						return config.ErrorHandler(c, err)
					}
				} else {
					tgt = config.Balancer.Next(c)
				}

				c.Set(config.ContextKey, tgt)

				//If retrying a failed request, clear any previous errors from
				//context here so that balancers have the option to check for
				//errors that occurred using previous target
				if retries < config.RetryCount {
					c.Set("_error", nil)
				}

				// This is needed for ProxyConfig.ModifyResponse and/or ProxyConfig.Transport to be able to process the Request
				// that Balancer may have replaced with c.SetRequest.
				req = c.Request()

				// Proxy
				switch {
				case c.IsWebSocket():
					proxyRaw(tgt, c, config).ServeHTTP(res, req)
				default: // even SSE requests
					proxyHTTP(tgt, c, config).ServeHTTP(res, req)
				}

				err, hasError := c.Get("_error").(error)
				if !hasError {
					return nil
				}

				retry := retries > 0 && config.RetryFilter(c, err)
				if !retry {
					return config.ErrorHandler(c, err)
				}

				retries--
			}
		}
	}
}

// StatusCodeContextCanceled is a custom HTTP status code for situations
// where a client unexpectedly closed the connection to the server.
// As there is no standard error code for "client closed connection", but
// various well-known HTTP clients and server implement this HTTP code we use
// 499 too instead of the more problematic 5xx, which does not allow to detect this situation
const StatusCodeContextCanceled = 499

func proxyHTTP(tgt *ProxyTarget, c echo.Context, config ProxyConfig) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(tgt.URL)
	proxy.ErrorHandler = func(resp http.ResponseWriter, req *http.Request, err error) {
		desc := tgt.URL.String()
		if tgt.Name != "" {
			desc = fmt.Sprintf("%s(%s)", tgt.Name, tgt.URL.String())
		}
		// If the client canceled the request (usually by closing the connection), we can report a
		// client error (4xx) instead of a server error (5xx) to correctly identify the situation.
		// The Go standard library (at of late 2020) wraps the exported, standard
		// context.Canceled error with unexported garbage value requiring a substring check, see
		// https://github.com/golang/go/blob/6965b01ea248cabb70c3749fd218b36089a21efb/src/net/net.go#L416-L430
		if err == context.Canceled || strings.Contains(err.Error(), "operation was canceled") {
			httpError := echo.NewHTTPError(StatusCodeContextCanceled, fmt.Sprintf("client closed connection: %v", err))
			httpError.Internal = err
			c.Set("_error", httpError)
		} else {
			httpError := echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("remote %s unreachable, could not forward: %v", desc, err))
			httpError.Internal = err
			c.Set("_error", httpError)
		}
	}
	proxy.Transport = config.Transport
	proxy.ModifyResponse = config.ModifyResponse
	return proxy
}
