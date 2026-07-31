# Changelog

## v4.15.2 - 2026-05-01

**Security**

* `Context.Scheme()` should validate values taken from header by @aldas in https://github.com/labstack/echo/pull/2962

Thanks to @shblue21 for reporting this [issue](https://github.com/labstack/echo/issues/2952).


## v4.15.1 - 2026-02-22

**Enhancements**

* CSRF: support older token-based CSRF protection handler that want to render token into template by @aldas in https://github.com/labstack/echo/pull/2905


## v4.15.0 - 2026-01-01


**Security**

NB: **If your application relies on cross-origin or same-site (same subdomain) requests do not blindly push this version to production**


The CSRF middleware now supports the [**Sec-Fetch-Site**](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Fetch-Site) header as a modern, defense-in-depth approach to [CSRF
protection](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#fetch-metadata-headers), implementing the OWASP-recommended Fetch Metadata API alongside the traditional token-based mechanism.

**How it works:**

Modern browsers automatically send the `Sec-Fetch-Site` header with all requests, indicating the relationship
between the request origin and the target. The middleware uses this to make security decisions:

- **`same-origin`** or **`none`**: Requests are allowed (exact origin match or direct user navigation)
- **`same-site`**: Falls back to token validation (e.g., subdomain to main domain)
- **`cross-site`**: Blocked by default with 403 error for unsafe methods (POST, PUT, DELETE, PATCH)

For browsers that don't send this header (older browsers), the middleware seamlessly falls back to
traditional token-based CSRF protection.

**New Configuration Options:**
- `TrustedOrigins []string`: Allowlist specific origins for cross-site requests (useful for OAuth callbacks, webhooks)
- `AllowSecFetchSiteFunc func(echo.Context) (bool, error)`: Custom logic for same-site/cross-site request validation

**Example:**
  ```go
  e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
      // Allow OAuth callbacks from trusted provider
      TrustedOrigins: []string{"https://oauth-provider.com"},

      // Custom validation for same-site requests
      AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
          // Your custom authorization logic here
          return validateCustomAuth(c), nil
          // return true, err  // blocks request with error
          // return true, nil  // allows CSRF request through
          // return false, nil // falls back to legacy token logic
      },
  }))
  ```
PR: https://github.com/labstack/echo/pull/2858

**Type-Safe Generic Parameter Binding**

* Added generic functions for type-safe parameter extraction and context access by @aldas in https://github.com/labstack/echo/pull/2856

  Echo now provides generic functions for extracting path, query, and form parameters with automatic type conversion,
  eliminating manual string parsing and type assertions.

  **New Functions:**
  - Path parameters: `PathParam[T]`, `PathParamOr[T]`
  - Query parameters: `QueryParam[T]`, `QueryParamOr[T]`, `QueryParams[T]`, `QueryParamsOr[T]`
  - Form values: `FormParam[T]`, `FormParamOr[T]`, `FormParams[T]`, `FormParamsOr[T]`
  - Context store: `ContextGet[T]`, `ContextGetOr[T]`

  **Supported Types:**
  Primitives (`bool`, `string`, `int`/`uint` variants, `float32`/`float64`), `time.Duration`, `time.Time`
  (with custom layouts and Unix timestamp support), and custom types implementing `BindUnmarshaler`,
  `TextUnmarshaler`, or `JSONUnmarshaler`.

  **Example:**
  ```go
  // Before: Manual parsing
  idStr := c.Param("id")
  id, err := strconv.Atoi(idStr)

  // After: Type-safe with automatic parsing
  id, err := echo.PathParam[int](c, "id")

  // With default values
  page, err := echo.QueryParamOr[int](c, "page", 1)
  limit, err := echo.QueryParamOr[int](c, "limit", 20)

  // Type-safe context access (no more panics from type assertions)
  user, err := echo.ContextGet[*User](c, "user")
  ```
  
PR: https://github.com/labstack/echo/pull/2856



**DEPRECATION NOTICE** Timeout Middleware Deprecated - Use ContextTimeout Instead

The `middleware.Timeout` middleware has been **deprecated** due to fundamental architectural issues that cause
data races. Use `middleware.ContextTimeout` or `middleware.ContextTimeoutWithConfig` instead.

**Why is this being deprecated?**

The Timeout middleware manipulates response writers across goroutine boundaries, which causes data races that
cannot be reliably fixed without a complete architectural redesign. The middleware:

- Swaps the response writer using `http.TimeoutHandler`
- Must be the first middleware in the chain (fragile constraint)
- Can cause races with other middleware (Logger, metrics, custom middleware)
- Has been the source of multiple race condition fixes over the years

**What should you use instead?**

The `ContextTimeout` middleware (available since v4.12.0) provides timeout functionality using Go's standard
context mechanism. It is:

- Race-free by design
- Can be placed anywhere in the middleware chain
- Simpler and more maintainable
- Compatible with all other middleware

**Migration Guide:**

```go
// Before (deprecated):
e.Use(middleware.Timeout())

// After (recommended):
e.Use(middleware.ContextTimeout(30 * time.Second))
```

**Important Behavioral Differences:**

1. **Handler cooperation required**: With ContextTimeout, your handlers must check `context.Done()` for cooperative
   cancellation. The old Timeout middleware would send a 503 response regardless of handler cooperation, but had
   data race issues.

2. **Error handling**: ContextTimeout returns errors through the standard error handling flow. Handlers that receive
   `context.DeadlineExceeded` should handle it appropriately:

```go
e.GET("/long-task", func(c echo.Context) error {
    ctx := c.Request().Context()

    // Example: database query with context
    result, err := db.QueryContext(ctx, "SELECT * FROM large_table")
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            // Handle timeout
            return echo.NewHTTPError(http.StatusServiceUnavailable, "Request timeout")
        }
        return err
    }

    return c.JSON(http.StatusOK, result)
})
```

3. **Background tasks**: For long-running background tasks, use goroutines with context:

```go
e.GET("/async-task", func(c echo.Context) error {
    ctx := c.Request().Context()

    resultCh := make(chan Result, 1)
    errCh := make(chan error, 1)

    go func() {
        result, err := performLongTask(ctx)
        if err != nil {
            errCh <- err
            return
        }
        resultCh <- result
    }()

    select {
    case result := <-resultCh:
        return c.JSON(http.StatusOK, result)
    case err := <-errCh:
        return err
    case <-ctx.Done():
        return echo.NewHTTPError(http.StatusServiceUnavailable, "Request timeout")
    }
})
```

**Enhancements**

* Fixes by @aldas in https://github.com/labstack/echo/pull/2852
* Generic functions by @aldas in https://github.com/labstack/echo/pull/2856
* CRSF with Sec-Fetch-Site checks by @aldas in https://github.com/labstack/echo/pull/2858


## v4.14.0 - 2025-12-11

`middleware.Logger` has been deprecated. For request logging, use `middleware.RequestLogger` or
`middleware.RequestLoggerWithConfig`.

`middleware.RequestLogger` replaces `middleware.Logger`, offering comparable configuration while relying on the
Go standard library’s new `slog` logger.

The previous default output format was JSON. The new default follows the standard `slog` logger settings.
To continue emitting request logs in JSON, configure `slog` accordingly:
```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
e.Use(middleware.RequestLogger())
```


**Security**

* Logger middleware json string escaping and deprecation by @aldas in https://github.com/labstack/echo/pull/2849



**Enhancements**

* Update deps  by @aldas in https://github.com/labstack/echo/pull/2807
* refactor to use reflect.TypeFor by @cuiweixie in https://github.com/labstack/echo/pull/2812
* Use Go 1.25 in CI by @aldas in https://github.com/labstack/echo/pull/2810
* Modernize context.go by replacing interface{} with any by @vishr in https://github.com/labstack/echo/pull/2822
* Fix typo in SetParamValues comment by @vishr in https://github.com/labstack/echo/pull/2828
* Fix typo in ContextTimeout middleware comment by @vishr in https://github.com/labstack/echo/pull/2827
* Improve BasicAuth middleware: use strings.Cut and RFC compliance by @vishr in https://github.com/labstack/echo/pull/2825
* Fix duplicate plus operator in router backtracking logic by @yuya-morimoto in https://github.com/labstack/echo/pull/2832
* Replace custom private IP range check with built-in net.IP.IsPrivate by @kumapower17 in https://github.com/labstack/echo/pull/2835
* Ensure proxy connection is closed in proxyRaw function(#2837) by @kumapower17 in https://github.com/labstack/echo/pull/2838
* Update deps by @aldas in https://github.com/labstack/echo/pull/2843
* Update golang.org/x/* deps by @aldas in https://github.com/labstack/echo/pull/2850



## v4.13.4 - 2025-05-22

**Enhancements**

* chore: fix some typos in comment by @zhuhaicity in https://github.com/labstack/echo/pull/2735
* CI: test with Go 1.24 by @aldas in https://github.com/labstack/echo/pull/2748
* Add support for TLS WebSocket proxy by @t-ibayashi-safie in https://github.com/labstack/echo/pull/2762

**Security**

* Update dependencies for [GO-2025-3487](https://pkg.go.dev/vuln/GO-2025-3487), [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503) and [GO-2025-3595](https://pkg.go.dev/vuln/GO-2025-3595) in https://github.com/labstack/echo/pull/2780


## v4.13.3 - 2024-12-19

**Security**

* Update golang.org/x/net dependency [GO-2024-3333](https://pkg.go.dev/vuln/GO-2024-3333) in https://github.com/labstack/echo/pull/2722


## v4.13.2 - 2024-12-12

**Security**

* Update dependencies (dependabot reports [GO-2024-3321](https://pkg.go.dev/vuln/GO-2024-3321)) in https://github.com/labstack/echo/pull/2721


## v4.13.1 - 2024-12-11

**Fixes**

* Fix BindBody ignoring `Transfer-Encoding: chunked` requests by @178inaba in https://github.com/labstack/echo/pull/2717



## v4.13.0 - 2024-12-04

**BREAKING CHANGE** JWT Middleware Removed from Core use [labstack/echo-jwt](https://github.com/labstack/echo-jwt) instead

The JWT middleware has been **removed from Echo core** due to another security vulnerability, [CVE-2024-51744](https://nvd.nist.gov/vuln/detail/CVE-2024-51744). For more details, refer to issue [#2699](https://github.com/labstack/echo/issues/2699). A drop-in replacement is available in the [labstack/echo-jwt](https://github.com/labstack/echo-jwt) repository.

**Important**: Direct assignments like `token := c.Get("user").(*jwt.Token)` will now cause a panic due to an invalid cast. Update your code accordingly. Replace the current imports from `"github.com/golang-jwt/jwt"` in your handlers to the new middleware version using `"github.com/golang-jwt/jwt/v5"`.


Background: 

The version of `golang-jwt/jwt` (v3.2.2) previously used in Echo core has been in an unmaintained state for some time. This is not the first vulnerability affecting this library; earlier issues were addressed in [PR #1946](https://github.com/labstack/echo/pull/1946).
JWT middleware was marked as deprecated in Echo core as of [v4.10.0](https://github.com/labstack/echo/releases/tag/v4.10.0) on 2022-12-27. If you did not notice that, consider leveraging tools like [Staticcheck](https://staticcheck.dev/) to catch such deprecations earlier in you dev/CI flow.  For bonus points - check out [gosec](https://github.com/securego/gosec).

We sincerely apologize for any inconvenience caused by this change. While we strive to maintain backward compatibility within Echo core, recurring security issues with third-party dependencies have forced this decision.

**Enhancements**

* remove jwt middleware by @stevenwhitehead in https://github.com/labstack/echo/pull/2701
* optimization: struct alignment by @behnambm in https://github.com/labstack/echo/pull/2636
* bind: Maintain backwards compatibility for map[string]interface{} binding by @thesaltree in https://github.com/labstack/echo/pull/2656
* Add Go 1.23 to CI by @aldas in https://github.com/labstack/echo/pull/2675
* improve `MultipartForm` test by @martinyonatann in https://github.com/labstack/echo/pull/2682
* `bind` : add support of multipart multi files by @martinyonatann in https://github.com/labstack/echo/pull/2684
* Add TemplateRenderer struct to ease creating renderers for `html/template` and `text/template` packages. by @aldas in https://github.com/labstack/echo/pull/2690
* Refactor TestBasicAuth to utilize table-driven test format by @ErikOlson in https://github.com/labstack/echo/pull/2688
* Remove broken header by @aldas in https://github.com/labstack/echo/pull/2705
* fix(bind body): content-length can be -1 by @phamvinhdat in https://github.com/labstack/echo/pull/2710
* CORS middleware should compile allowOrigin regexp at creation by @aldas in https://github.com/labstack/echo/pull/2709
* Shorten Github issue template and add test example by @aldas in https://github.com/labstack/echo/pull/2711


## v4.12.0 - 2024-04-15

**Security**

* Update golang.org/x/net dep because of [GO-2024-2687](https://pkg.go.dev/vuln/GO-2024-2687) by @aldas in https://github.com/labstack/echo/pull/2625


**Enhancements**

* binder: make binding to Map work better with string destinations by @aldas in https://github.com/labstack/echo/pull/2554
* README.md: add Encore as sponsor by @marcuskohlberg in https://github.com/labstack/echo/pull/2579
* Reorder paragraphs in README.md by @aldas in https://github.com/labstack/echo/pull/2581
* CI: upgrade actions/checkout to v4 by @aldas in https://github.com/labstack/echo/pull/2584
* Remove default charset from 'application/json' Content-Type header by @doortts in https://github.com/labstack/echo/pull/2568
* CI: Use Go 1.22 by @aldas in https://github.com/labstack/echo/pull/2588
* binder: allow binding to a nil map by @georgmu in https://github.com/labstack/echo/pull/2574
* Add Skipper Unit Test In BasicBasicAuthConfig and Add More Detail Explanation regarding BasicAuthValidator by @RyoKusnadi in https://github.com/labstack/echo/pull/2461
* fix some typos by @teslaedison in https://github.com/labstack/echo/pull/2603
* fix: some typos by @pomadev in https://github.com/labstack/echo/pull/2596
* Allow ResponseWriters to unwrap writers when flushing/hijacking by @aldas in https://github.com/labstack/echo/pull/2595
* Add SPDX licence comments to files.  by @aldas in https://github.com/labstack/echo/pull/2604
* Upgrade deps by @aldas in https://github.com/labstack/echo/pull/2605
* Change type definition blocks to single declarations. This helps copy… by @aldas in https://github.com/labstack/echo/pull/2606
* Fix Real IP logic by @cl-bvl in https://github.com/labstack/echo/pull/2550
* Default binder can use `UnmarshalParams(params []string) error` inter… by @aldas in https://github.com/labstack/echo/pull/2607
* Default binder can bind pointer to slice as struct field. For example  `*[]string` by @aldas in https://github.com/labstack/echo/pull/2608
* Remove maxparam dependence from Context by @aldas in https://github.com/labstack/echo/pull/2611
* When route is registered with empty path it is normalized to `/`.  by @aldas in https://github.com/labstack/echo/pull/2616
* proxy middleware should use httputil.ReverseProxy for SSE requests by @aldas in https://github.com/labstack/echo/pull/2624


## v4.11.4 - 2023-12-20

**Security**

* Upgrade golang.org/x/crypto to v0.17.0 to fix vulnerability [issue](https://pkg.go.dev/vuln/GO-2023-2402) [#2562](https://github.com/labstack/echo/pull/2562)

**Enhancements**

* Update deps and mark Go version to 1.18 as this is what golang.org/x/* use [#2563](https://github.com/labstack/echo/pull/2563)
* Request logger: add example for Slog https://pkg.go.dev/log/slog [#2543](https://github.com/labstack/echo/pull/2543)


## v4.11.3 - 2023-11-07

**Security**

* 'c.Attachment' and 'c.Inline' should escape filename in 'Content-Disposition' header to avoid 'Reflect File Download' vulnerability. [#2541](https://github.com/labstack/echo/pull/2541)

**Enhancements**

* Tests: refactor context tests to be separate functions [#2540](https://github.com/labstack/echo/pull/2540)
* Proxy middleware: reuse echo request context [#2537](https://github.com/labstack/echo/pull/2537)
* Mark unmarshallable yaml struct tags as ignored [#2536](https://github.com/labstack/echo/pull/2536)


## v4.11.2 - 2023-10-11

**Security**

* Bump golang.org/x/net to prevent CVE-2023-39325 / CVE-2023-44487 HTTP/2 Rapid Reset Attack [#2527](https://github.com/labstack/echo/pull/2527)
* fix(sec): randomString bias introduced by #2490 [#2492](https://github.com/labstack/echo/pull/2492)
* CSRF/RequestID mw: switch math/random usage to crypto/random [#2490](https://github.com/labstack/echo/pull/2490)

**Enhancements**

* Delete unused context in body_limit.go [#2483](https://github.com/labstack/echo/pull/2483)
* Use Go 1.21 in CI [#2505](https://github.com/labstack/echo/pull/2505)
* Fix some typos [#2511](https://github.com/labstack/echo/pull/2511)
* Allow CORS middleware to send Access-Control-Max-Age: 0 [#2518](https://github.com/labstack/echo/pull/2518)
* Bump dependancies [#2522](https://github.com/labstack/echo/pull/2522)

## v4.11.1 - 2023-07-16

**Fixes**

* Fix `Gzip` middleware not sending response code for no content responses (404, 301/302 redirects etc) [#2481](https://github.com/labstack/echo/pull/2481)


## v4.11.0 - 2023-07-14


**Fixes**

* Fixes the proxy middleware concurrency issue of calling the Next() proxy target on Round Robin Balancer [#2409](https://github.com/labstack/echo/pull/2409)
* Fix `group.RouteNotFound` not working when group has attached middlewares [#2411](https://github.com/labstack/echo/pull/2411)
* Fix global error handler return error message when message is an error [#2456](https://github.com/labstack/echo/pull/2456)
* Do not use global timeNow variables [#2477](https://github.com/labstack/echo/pull/2477)


**Enhancements**

* Added a optional config variable to disable centralized error handler in recovery middleware [#2410](https://github.com/labstack/echo/pull/2410)
* refactor: use `strings.ReplaceAll` directly [#2424](https://github.com/labstack/echo/pull/2424)
* Add support for Go1.20 `http.rwUnwrapper` to Response struct [#2425](https://github.com/labstack/echo/pull/2425)
* Check whether is nil before invoking centralized error handling [#2429](https://github.com/labstack/echo/pull/2429)
* Proper colon support in `echo.Reverse` method [#2416](https://github.com/labstack/echo/pull/2416)
* Fix misuses of a vs an in documentation comments [#2436](https://github.com/labstack/echo/pull/2436)
* Add link to slog.Handler library for Echo logging into README.md [#2444](https://github.com/labstack/echo/pull/2444)
* In proxy middleware Support retries of failed proxy requests [#2414](https://github.com/labstack/echo/pull/2414)
* gofmt fixes to comments [#2452](https://github.com/labstack/echo/pull/2452)
* gzip response only if it exceeds a minimal length [#2267](https://github.com/labstack/echo/pull/2267)
* Upgrade packages [#2475](https://github.com/labstack/echo/pull/2475)


## v4.10.2 - 2023-02-22

**Security**

* `filepath.Clean` behaviour has changed in Go 1.20 - adapt to it [#2406](https://github.com/labstack/echo/pull/2406)
* Add `middleware.CORSConfig.UnsafeWildcardOriginWithAllowCredentials` to make UNSAFE usages of wildcard origin + allow cretentials less likely [#2405](https://github.com/labstack/echo/pull/2405)

**Enhancements**

* Add more HTTP error values [#2277](https://github.com/labstack/echo/pull/2277)


## v4.10.1 - 2023-02-19

**Security**

* Upgrade deps due to the latest golang.org/x/net vulnerability [#2402](https://github.com/labstack/echo/pull/2402)


**Enhancements**

* Add new JWT repository to the README [#2377](https://github.com/labstack/echo/pull/2377)
* Return an empty string for ctx.path if there is no registered path [#2385](https://github.com/labstack/echo/pull/2385)
* Add context timeout middleware [#2380](https://github.com/labstack/echo/pull/2380)
* Update link to jaegertracing [#2394](https://github.com/labstack/echo/pull/2394)


## v4.10.0 - 2022-12-27

**Security**

* We are deprecating JWT middleware in this repository. Please use https://github.com/labstack/echo-jwt instead. 

  JWT middleware is moved to separate repository to allow us to bump/upgrade version of JWT implementation (`github.com/golang-jwt/jwt`) we are using
which we can not do in Echo core because this would break backwards compatibility guarantees we try to maintain.

* This minor version bumps minimum Go version to 1.17 (from 1.16) due `golang.org/x/` packages we depend on. There are
  several vulnerabilities fixed in these libraries.

  Echo still tries to support last 4 Go versions but there are occasions we can not guarantee this promise.


**Enhancements**

* Bump x/text to 0.3.8 [#2305](https://github.com/labstack/echo/pull/2305)
* Bump dependencies and add notes about Go releases we support [#2336](https://github.com/labstack/echo/pull/2336)
* Add helper interface for ProxyBalancer interface [#2316](https://github.com/labstack/echo/pull/2316)
* Expose `middleware.CreateExtractors` function so we can use it from echo-contrib repository [#2338](https://github.com/labstack/echo/pull/2338)
* Refactor func(Context) error to HandlerFunc [#2315](https://github.com/labstack/echo/pull/2315)
* Improve function comments [#2329](https://github.com/labstack/echo/pull/2329)
* Add new method HTTPError.WithInternal [#2340](https://github.com/labstack/echo/pull/2340)
* Replace io/ioutil package usages [#2342](https://github.com/labstack/echo/pull/2342)
* Add staticcheck to CI flow [#2343](https://github.com/labstack/echo/pull/2343)
* Replace relative path determination from proprietary to std [#2345](https://github.com/labstack/echo/pull/2345)
* Remove square brackets from ipv6 addresses in XFF (X-Forwarded-For header) [#2182](https://github.com/labstack/echo/pull/2182)
* Add testcases for some BodyLimit middleware configuration options [#2350](https://github.com/labstack/echo/pull/2350)
* Additional configuration options for RequestLogger and Logger middleware [#2341](https://github.com/labstack/echo/pull/2341)
* Add route to request log [#2162](https://github.com/labstack/echo/pull/2162)
* GitHub Workflows security hardening [#2358](https://github.com/labstack/echo/pull/2358)
* Add govulncheck to CI and bump dependencies [#2362](https://github.com/labstack/echo/pull/2362)
* Fix rate limiter docs [#2366](https://github.com/labstack/echo/pull/2366)
* Refactor how `e.Routes()` work and introduce `e.OnAddRouteHandler` callback [#2337](https://github.com/labstack/echo/pull/2337)


## v4.9.1 - 2022-10-12

**Fixes**

* Fix logger panicing (when template is set to empty) by bumping dependency version [#2295](https://github.com/labstack/echo/issues/2295)

**Enhancements**

* Improve CORS documentation [#2272](https://github.com/labstack/echo/pull/2272)
* Update readme about supported Go versions [#2291](https://github.com/labstack/echo/pull/2291)
* Tests: improve error handling on closing body [#2254](https://github.com/labstack/echo/pull/2254)
* Tests: refactor some of the assertions in tests [#2275](https://github.com/labstack/echo/pull/2275)
* Tests: refactor assertions [#2301](https://github.com/labstack/echo/pull/2301)

## v4.9.0 - 2022-09-04

**Security**

* Fix open redirect vulnerability in handlers serving static directories (e.Static, e.StaticFs, echo.StaticDirectoryHandler) [#2260](https://github.com/labstack/echo/pull/2260)

**Enhancements**

* Allow configuring ErrorHandler in CSRF middleware [#2257](https://github.com/labstack/echo/pull/2257)
* Replace HTTP method constants in tests with stdlib constants [#2247](https://github.com/labstack/echo/pull/2247)


## v4.8.0 - 2022-08-10

**Most notable things**

You can now add any arbitrary HTTP method type as a route [#2237](https://github.com/labstack/echo/pull/2237)
```go
e.Add("COPY", "/*", func(c echo.Context) error 
  return c.String(http.StatusOK, "OK COPY")
})
```

You can add custom 404 handler for specific paths [#2217](https://github.com/labstack/echo/pull/2217)
```go
e.RouteNotFound("/*", func(c echo.Context) error { return c.NoContent(http.StatusNotFound) })

g := e.Group("/images")
g.RouteNotFound("/*", func(c echo.Context) error { return c.NoContent(http.StatusNotFound) })
```

**Enhancements**

* Add new value binding methods (UnixTimeMilli,TextUnmarshaler,JSONUnmarshaler) to Valuebinder [#2127](https://github.com/labstack/echo/pull/2127)
* Refactor: body_limit middleware unit test [#2145](https://github.com/labstack/echo/pull/2145)
* Refactor: Timeout mw: rework how test waits for timeout. [#2187](https://github.com/labstack/echo/pull/2187)
* BasicAuth middleware returns 500 InternalServerError on invalid base64 strings but should return 400 [#2191](https://github.com/labstack/echo/pull/2191)
* Refactor: duplicated findStaticChild process at findChildWithLabel [#2176](https://github.com/labstack/echo/pull/2176)
* Allow different param names in different methods with same path scheme [#2209](https://github.com/labstack/echo/pull/2209)
* Add support for registering handlers for different 404 routes [#2217](https://github.com/labstack/echo/pull/2217)
* Middlewares should use errors.As() instead of type assertion on HTTPError [#2227](https://github.com/labstack/echo/pull/2227)
* Allow arbitrary HTTP method types to be added as routes [#2237](https://github.com/labstack/echo/pull/2237)

## v4.7.2 - 2022-03-16

**Fixes**

* Fix nil pointer exception when calling Start again after address binding error [#2131](https://github.com/labstack/echo/pull/2131)
* Fix CSRF middleware not being able to extract token from multipart/form-data form [#2136](https://github.com/labstack/echo/pull/2136)
* Fix Timeout middleware write race [#2126](https://github.com/labstack/echo/pull/2126)

**Enhancements**

* Recover middleware should not log panic for aborted handler [#2134](https://github.com/labstack/echo/pull/2134)


## v4.7.1 - 2022-03-13

**Fixes**

* Fix `e.Static`, `.File()`, `c.Attachment()` being picky with paths starting with `./`, `../` and `/` after 4.7.0 introduced echo.Filesystem support (Go1.16+) [#2123](https://github.com/labstack/echo/pull/2123)

**Enhancements**

* Remove some unused code [#2116](https://github.com/labstack/echo/pull/2116)


## v4.7.0 - 2022-03-01

**Enhancements**

* Add JWT, KeyAuth, CSRF multivalue extractors [#2060](https://github.com/labstack/echo/pull/2060)
* Add LogErrorFunc to recover middleware [#2072](https://github.com/labstack/echo/pull/2072)
* Add support for HEAD method query params binding [#2027](https://github.com/labstack/echo/pull/2027)
* Improve filesystem support with echo.FileFS, echo.StaticFS, group.FileFS, group.StaticFS [#2064](https://github.com/labstack/echo/pull/2064)

**Fixes**

* Fix X-Real-IP bug, improve tests [#2007](https://github.com/labstack/echo/pull/2007)
* Minor syntax fixes [#1994](https://github.com/labstack/echo/pull/1994), [#2102](https://github.com/labstack/echo/pull/2102), [#2102](https://github.com/labstack/echo/pull/2102)

**General**

* Add cache-control and connection headers [#2103](https://github.com/labstack/echo/pull/2103)
* Add Retry-After header constant [#2078](https://github.com/labstack/echo/pull/2078)
* Upgrade `go` directive in `go.mod` to 1.17 [#2049](https://github.com/labstack/echo/pull/2049)
* Add Pagoda [#2077](https://github.com/labstack/echo/pull/2077) and Souin [#2069](https://github.com/labstack/echo/pull/2069) to 3rd-party middlewares in README

## v4.6.3 - 2022-01-10

**Fixes**

* Fixed Echo version number in greeting message which was not incremented to `4.6.2` [#2066](https://github.com/labstack/echo/issues/2066)


## v4.6.2 - 2022-01-08

**Fixes**

* Fixed route containing escaped colon should be matchable but is not matched to request path [#2047](https://github.com/labstack/echo/pull/2047)
* Fixed a problem that returned wrong content-encoding when the gzip compressed content was empty. [#1921](https://github.com/labstack/echo/pull/1921)
* Update (test) dependencies [#2021](https://github.com/labstack/echo/pull/2021)


**Enhancements**

* Add support for configurable target header for the request_id middleware [#2040](https://github.com/labstack/echo/pull/2040)
* Change decompress middleware to use stream decompression instead of buffering [#2018](https://github.com/labstack/echo/pull/2018)
* Documentation updates


## v4.6.1 - 2021-09-26

**Enhancements**

* Add start time to request logger middleware values [#1991](https://github.com/labstack/echo/pull/1991)

## v4.6.0 - 2021-09-20

Introduced a new [request logger](https://github.com/labstack/echo/blob/master/middleware/request_logger.go) middleware 
to help with cases when you want to use some other logging library in your application.

**Fixes**

* fix timeout middleware warning: superfluous response.WriteHeader [#1905](https://github.com/labstack/echo/issues/1905)

**Enhancements**

* Add Cookie to KeyAuth middleware's KeyLookup [#1929](https://github.com/labstack/echo/pull/1929)
* JWT middleware should ignore case of auth scheme in request header [#1951](https://github.com/labstack/echo/pull/1951)
* Refactor default error handler to return first if response is already committed [#1956](https://github.com/labstack/echo/pull/1956)
* Added request logger middleware which helps to use custom logger library for logging requests. [#1980](https://github.com/labstack/echo/pull/1980)
* Allow escaping of colon in route path so Google Cloud API "custom methods" could be implemented [#1988](https://github.com/labstack/echo/pull/1988)

## v4.5.0 - 2021-08-01

**Important notes**

A **BREAKING CHANGE** is introduced for JWT middleware users.
The JWT library used for the JWT middleware had to be changed from [github.com/dgrijalva/jwt-go](https://github.com/dgrijalva/jwt-go) to
[github.com/golang-jwt/jwt](https://github.com/golang-jwt/jwt) due former library being unmaintained and affected by security
issues.
The [github.com/golang-jwt/jwt](https://github.com/golang-jwt/jwt) project is a drop-in replacement, but supports only the latest 2 Go versions.
So for JWT middleware users Go 1.15+ is required. For detailed information please read [#1940](https://github.com/labstack/echo/discussions/)

To change the library imports in all .go files in your project replace all occurrences of `dgrijalva/jwt-go` with `golang-jwt/jwt`.

For Linux CLI you can use:
```bash
find -type f -name "*.go" -exec sed -i "s/dgrijalva\/jwt-go/golang-jwt\/jwt/g" {} \;
go mod tidy
```

**Fixes**

* Change JWT library to `github.com/golang-jwt/jwt` [#1946](https://github.com/labstack/echo/pull/1946)

## v4.4.0 - 2021-07-12

**Fixes**

* Split HeaderXForwardedFor header only by comma [#1878](https://github.com/labstack/echo/pull/1878)
* Fix Timeout middleware Context propagation [#1910](https://github.com/labstack/echo/pull/1910)
 
**Enhancements**

* Bind data using headers as source [#1866](https://github.com/labstack/echo/pull/1866)
* Adds JWTConfig.ParseTokenFunc to JWT middleware to allow different libraries implementing JWT parsing. [#1887](https://github.com/labstack/echo/pull/1887)
* Adding tests for Echo#Host [#1895](https://github.com/labstack/echo/pull/1895)
* Adds RequestIDHandler function to RequestID middleware [#1898](https://github.com/labstack/echo/pull/1898)
* Allow for custom JSON encoding implementations [#1880](https://github.com/labstack/echo/pull/1880)

## v4.3.0 - 2021-05-08

**Important notes**

* Route matching has improvements for following cases:
  1. Correctly match routes with parameter part as last part of route (with trailing backslash)
  2. Considering handlers when resolving routes and search for matching http method handler
* Echo minimal Go version is now 1.13. 

**Fixes**

* When url ends with slash first param route is the match [#1804](https://github.com/labstack/echo/pull/1812)
* Router should check if node is suitable as matching route by path+method and if not then continue search in tree [#1808](https://github.com/labstack/echo/issues/1808)
* Fix timeout middleware not writing response correctly when handler panics [#1864](https://github.com/labstack/echo/pull/1864)
* Fix binder not working with embedded pointer structs [#1861](https://github.com/labstack/echo/pull/1861)
* Add Go 1.16 to CI and drop 1.12 specific code [#1850](https://github.com/labstack/echo/pull/1850)

**Enhancements**

* Make KeyFunc public in JWT middleware [#1756](https://github.com/labstack/echo/pull/1756)
* Add support for optional filesystem to the static middleware [#1797](https://github.com/labstack/echo/pull/1797)
* Add a custom error handler to key-auth middleware [#1847](https://github.com/labstack/echo/pull/1847)
* Allow JWT token to be looked up from multiple sources [#1845](https://github.com/labstack/echo/pull/1845)

## v4.2.2 - 2021-04-07

**Fixes**

* Allow proxy middleware to use query part in rewrite (#1802)
* Fix timeout middleware not sending status code when handler returns an error (#1805)
* Fix Bind() when target is array/slice and path/query params complains bind target not being struct (#1835)
* Fix panic in redirect middleware on short host name (#1813)
* Fix timeout middleware docs (#1836)

## v4.2.1 - 2021-03-08

**Important notes**

Due to a datarace the config parameters for the newly added timeout middleware required a change.
See the [docs](https://echo.labstack.com/middleware/timeout).
A performance regression has been fixed, even bringing better performance than before for some routing scenarios.

**Fixes**

* Fix performance regression caused by path escaping (#1777, #1798, #1799, aldas)
* Avoid context canceled errors (#1789, clwluvw)
* Improve router to use on stack backtracking (#1791, aldas, stffabi)
* Fix panic in timeout middleware not being not recovered and cause application crash (#1794, aldas)
* Fix Echo.Serve() not serving on HTTP port correctly when TLSListener is used (#1785, #1793, aldas)
* Apply go fmt (#1788, Le0tk0k)
* Uses strings.Equalfold (#1790, rkilingr)
* Improve code quality (#1792, withshubh)

This release was made possible by our **contributors**:
aldas, clwluvw, lammel, Le0tk0k, maciej-jezierski, rkilingr, stffabi, withshubh

## v4.2.0 - 2021-02-11

**Important notes**

The behaviour for binding data has been reworked for compatibility with echo before v4.1.11 by
enforcing `explicit tagging` for processing parameters. This **may break** your code if you 
expect combined handling of query/path/form params.
Please see the updated documentation for [request](https://echo.labstack.com/guide/request) and [binding](https://echo.labstack.com/guide/request)

The handling for rewrite rules has been slightly adjusted to expand `*` to a non-greedy `(.*?)` capture group. This is only relevant if multiple asterisks are used in your rules.
Please see [rewrite](https://echo.labstack.com/middleware/rewrite) and [proxy](https://echo.labstack.com/middleware/proxy) for details.

**Security**

* Fix directory traversal vulnerability for Windows (#1718, little-cui)
* Fix open redirect vulnerability with trailing slash (#1771,#1775 aldas,GeoffreyFrogeye)

**Enhancements**

* Add Echo#ListenerNetwork as configuration (#1667, pafuent)
* Add ability to change the status code using response beforeFuncs (#1706, RashadAnsari)
* Echo server startup to allow data race free access to listener address
* Binder: Restore pre v4.1.11 behaviour for c.Bind() to use query params only for GET or DELETE methods (#1727, aldas)
* Binder: Add separate methods to bind only query params, path params or request body (#1681, aldas)
* Binder: New fluent binder for query/path/form parameter binding (#1717, #1736, aldas)
* Router: Performance improvements for missed routes (#1689, pafuent)
* Router: Improve performance for Real-IP detection using IndexByte instead of Split (#1640, imxyb)
* Middleware: Support real regex rules for rewrite and proxy middleware (#1767)
* Middleware: New rate limiting middleware (#1724, iambenkay)
* Middleware: New timeout middleware implementation for go1.13+ (#1743, )
* Middleware: Allow regex pattern for CORS middleware (#1623, KlotzAndrew)
* Middleware: Add IgnoreBase parameter to static middleware (#1701, lnenad, iambenkay)
* Middleware: Add an optional custom function to CORS middleware to validate origin (#1651, curvegrid)
* Middleware: Support form fields in JWT middleware (#1704, rkfg)
* Middleware: Use sync.Pool for (de)compress middleware to improve performance (#1699, #1672, pafuent)
* Middleware: Add decompress middleware to support gzip compressed requests (#1687, arun0009)
* Middleware: Add ErrJWTInvalid for JWT middleware (#1627, juanbelieni)
* Middleware: Add SameSite mode for CSRF cookies to support iframes (#1524, pr0head)

**Fixes**

* Fix handling of special trailing slash case for partial prefix (#1741, stffabi)
* Fix handling of static routes with trailing slash (#1747)
* Fix Static files route not working (#1671, pwli0755, lammel)
* Fix use of caret(^) in regex for rewrite middleware (#1588, chotow)
* Fix Echo#Reverse for Any type routes (#1695, pafuent)
* Fix Router#Find panic with infinite loop (#1661, pafuent)
* Fix Router#Find panic fails on Param paths (#1659, pafuent)
* Fix DefaultHTTPErrorHandler with Debug=true (#1477, lammel)
* Fix incorrect CORS headers (#1669, ulasakdeniz)
* Fix proxy middleware rewritePath to use url with updated tests (#1630, arun0009)
* Fix rewritePath for proxy middleware to use escaped path in (#1628, arun0009)
* Remove unless defer (#1656, imxyb)

**General**

* New maintainers for Echo: Roland Lammel (@lammel) and Pablo Andres Fuente (@pafuent)
* Add GitHub action to compare benchmarks (#1702, pafuent)
* Binding query/path params and form fields to struct only works for explicit tags (#1729,#1734, aldas)
* Add support for Go 1.15 in CI (#1683, asahasrabuddhe)
* Add test for request id to remain unchanged if provided (#1719, iambenkay)
* Refactor echo instance listener access and startup to speed up testing (#1735, aldas)
* Refactor and improve various tests for binding and routing
* Run test workflow only for relevant changes (#1637, #1636, pofl)
* Update .travis.yml (#1662, santosh653)
* Update README.md with an recents framework benchmark (#1679, pafuent)

This release was made possible by **over 100 commits** from more than **20 contributors**:
asahasrabuddhe, aldas, AndrewKlotz, arun0009, chotow, curvegrid, iambenkay, imxyb, 
juanbelieni,  lammel, little-cui, lnenad, pafuent, pofl, pr0head, pwli, RashadAnsari, 
rkfg, santosh653, segfiner, stffabi, ulasakdeniz
