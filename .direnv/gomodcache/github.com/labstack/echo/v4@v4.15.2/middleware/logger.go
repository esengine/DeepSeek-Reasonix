// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/color"
	"github.com/valyala/fasttemplate"
)

// LoggerConfig defines the config for Logger middleware.
//
// # Configuration Examples
//
// ## Basic Usage with Default Settings
//
//	e.Use(middleware.Logger())
//
// This uses the default JSON format that logs all common request/response details.
//
// ## Custom Simple Format
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: "${time_rfc3339_nano} ${status} ${method} ${uri} ${latency_human}\n",
//	}))
//
// ## JSON Format with Custom Fields
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: `{"timestamp":"${time_rfc3339_nano}","level":"info","remote_ip":"${remote_ip}",` +
//			`"method":"${method}","uri":"${uri}","status":${status},"latency":"${latency_human}",` +
//			`"user_agent":"${user_agent}","error":"${error}"}` + "\n",
//	}))
//
// ## Custom Time Format
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: "${time_custom} ${method} ${uri} ${status}\n",
//		CustomTimeFormat: "2006-01-02 15:04:05",
//	}))
//
// ## Logging Headers and Parameters
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: `{"time":"${time_rfc3339_nano}","method":"${method}","uri":"${uri}",` +
//			`"status":${status},"auth":"${header:Authorization}","user":"${query:user}",` +
//			`"form_data":"${form:action}","session":"${cookie:session_id}"}` + "\n",
//	}))
//
// ## Custom Output (File Logging)
//
//	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer file.Close()
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Output: file,
//	}))
//
// ## Custom Tag Function
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: `{"time":"${time_rfc3339_nano}","user_id":"${custom}","method":"${method}"}` + "\n",
//		CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
//			userID := getUserIDFromContext(c) // Your custom logic
//			return buf.WriteString(strconv.Itoa(userID))
//		},
//	}))
//
// ## Conditional Logging (Skip Certain Requests)
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Skipper: func(c echo.Context) bool {
//			// Skip logging for health check endpoints
//			return c.Request().URL.Path == "/health" || c.Request().URL.Path == "/metrics"
//		},
//	}))
//
// ## Integration with External Logging Service
//
//	logBuffer := &SyncBuffer{} // Thread-safe buffer for external service
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: `{"timestamp":"${time_rfc3339_nano}","service":"my-api","level":"info",` +
//			`"method":"${method}","uri":"${uri}","status":${status},"latency_ms":${latency},` +
//			`"remote_ip":"${remote_ip}","user_agent":"${user_agent}","error":"${error}"}` + "\n",
//		Output: logBuffer,
//	}))
//
// # Available Tags
//
// ## Time Tags
//   - time_unix: Unix timestamp (seconds)
//   - time_unix_milli: Unix timestamp (milliseconds)
//   - time_unix_micro: Unix timestamp (microseconds)
//   - time_unix_nano: Unix timestamp (nanoseconds)
//   - time_rfc3339: RFC3339 format (2006-01-02T15:04:05Z07:00)
//   - time_rfc3339_nano: RFC3339 with nanoseconds
//   - time_custom: Uses CustomTimeFormat field
//
// ## Request Information
//   - id: Request ID from X-Request-ID header
//   - remote_ip: Client IP address (respects proxy headers)
//   - uri: Full request URI with query parameters
//   - host: Host header value
//   - method: HTTP method (GET, POST, etc.)
//   - path: URL path without query parameters
//   - route: Echo route pattern (e.g., /users/:id)
//   - protocol: HTTP protocol version
//   - referer: Referer header value
//   - user_agent: User-Agent header value
//
// ## Response Information
//   - status: HTTP status code
//   - error: Error message if request failed
//   - latency: Request processing time in nanoseconds
//   - latency_human: Human-readable processing time
//   - bytes_in: Request body size in bytes
//   - bytes_out: Response body size in bytes
//
// ## Dynamic Tags
//   - header:<NAME>: Value of specific header (e.g., header:Authorization)
//   - query:<NAME>: Value of specific query parameter (e.g., query:user_id)
//   - form:<NAME>: Value of specific form field (e.g., form:username)
//   - cookie:<NAME>: Value of specific cookie (e.g., cookie:session_id)
//   - custom: Output from CustomTagFunc
//
// # Troubleshooting
//
// ## Common Issues
//
// 1. **Missing logs**: Check if Skipper function is filtering out requests
// 2. **Invalid JSON**: Ensure CustomTagFunc outputs valid JSON content
// 3. **Performance issues**: Consider using a buffered writer for high-traffic applications
// 4. **File permission errors**: Ensure write permissions when logging to files
//
// ## Performance Tips
//
// - Use time_unix formats for better performance than time_rfc3339
// - Minimize the number of dynamic tags (header:, query:, form:, cookie:)
// - Use Skipper to exclude high-frequency, low-value requests (health checks, etc.)
// - Consider async logging for very high-traffic applications
type LoggerConfig struct {
	// Skipper defines a function to skip middleware.
	// Use this to exclude certain requests from logging (e.g., health checks).
	//
	// Example:
	//	Skipper: func(c echo.Context) bool {
	//		return c.Request().URL.Path == "/health"
	//	},
	Skipper Skipper

	// Format defines the logging format using template tags.
	// Tags are enclosed in ${} and replaced with actual values.
	// See the detailed tag documentation above for all available options.
	//
	// Default: JSON format with common fields
	// Example: "${time_rfc3339_nano} ${status} ${method} ${uri} ${latency_human}\n"
	Format string `yaml:"format"`

	// CustomTimeFormat specifies the time format used by ${time_custom} tag.
	// Uses Go's reference time: Mon Jan 2 15:04:05 MST 2006
	//
	// Default: "2006-01-02 15:04:05.00000"
	// Example: "2006-01-02 15:04:05" or "15:04:05.000"
	CustomTimeFormat string `yaml:"custom_time_format"`

	// CustomTagFunc is called when ${custom} tag is encountered.
	// Use this to add application-specific information to logs.
	// The function should write valid content for your log format.
	//
	// Example:
	//	CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
	//		userID := getUserFromContext(c)
	//		return buf.WriteString(`"user_id":"` + userID + `"`)
	//	},
	CustomTagFunc func(c echo.Context, buf *bytes.Buffer) (int, error)

	// Output specifies where logs are written.
	// Can be any io.Writer: files, buffers, network connections, etc.
	//
	// Default: os.Stdout
	// Example: Custom file, syslog, or external logging service
	Output io.Writer

	template *fasttemplate.Template
	colorer  *color.Color
	pool     *sync.Pool
	timeNow  func() time.Time
}

// DefaultLoggerConfig is the default Logger middleware config.
var DefaultLoggerConfig = LoggerConfig{
	Skipper: DefaultSkipper,
	Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}",` +
		`"host":"${host}","method":"${method}","uri":"${uri}","user_agent":"${user_agent}",` +
		`"status":${status},"error":"${error}","latency":${latency},"latency_human":"${latency_human}"` +
		`,"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
	CustomTimeFormat: "2006-01-02 15:04:05.00000",
	colorer:          color.New(),
	timeNow:          time.Now,
}

// Logger returns a middleware that logs HTTP requests using the default configuration.
//
// The default format logs requests as JSON with the following fields:
//   - time: RFC3339 nano timestamp
//   - id: Request ID from X-Request-ID header
//   - remote_ip: Client IP address
//   - host: Host header
//   - method: HTTP method
//   - uri: Request URI
//   - user_agent: User-Agent header
//   - status: HTTP status code
//   - error: Error message (if any)
//   - latency: Processing time in nanoseconds
//   - latency_human: Human-readable processing time
//   - bytes_in: Request body size
//   - bytes_out: Response body size
//
// Example output:
//
//	{"time":"2023-01-15T10:30:45.123456789Z","id":"","remote_ip":"127.0.0.1",
//	"host":"localhost:8080","method":"GET","uri":"/users/123","user_agent":"curl/7.81.0",
//	"status":200,"error":"","latency":1234567,"latency_human":"1.234567ms",
//	"bytes_in":0,"bytes_out":42}
//
// For custom configurations, use LoggerWithConfig instead.
//
// Deprecated: please use middleware.RequestLogger or middleware.RequestLoggerWithConfig instead.
func Logger() echo.MiddlewareFunc {
	return LoggerWithConfig(DefaultLoggerConfig)
}

// LoggerWithConfig returns a Logger middleware with custom configuration.
//
// This function allows you to customize all aspects of request logging including:
//   - Log format and fields
//   - Output destination
//   - Time formatting
//   - Custom tags and logic
//   - Request filtering
//
// See LoggerConfig documentation for detailed configuration examples and options.
//
// Example:
//
//	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
//		Format: "${time_rfc3339} ${status} ${method} ${uri} ${latency_human}\n",
//		Output: customLogWriter,
//		Skipper: func(c echo.Context) bool {
//			return c.Request().URL.Path == "/health"
//		},
//	}))
//
// Deprecated: please use middleware.RequestLoggerWithConfig instead.
func LoggerWithConfig(config LoggerConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Skipper == nil {
		config.Skipper = DefaultLoggerConfig.Skipper
	}
	if config.Format == "" {
		config.Format = DefaultLoggerConfig.Format
	}
	writeString := func(buf *bytes.Buffer, in string) (int, error) { return buf.WriteString(in) }
	if config.Format[0] == '{' { // format looks like JSON, so we need to escape invalid characters
		writeString = writeJSONSafeString
	}

	if config.Output == nil {
		config.Output = DefaultLoggerConfig.Output
	}
	timeNow := DefaultLoggerConfig.timeNow
	if config.timeNow != nil {
		timeNow = config.timeNow
	}

	config.template = fasttemplate.New(config.Format, "${", "}")
	config.colorer = color.New()
	config.colorer.SetOutput(config.Output)
	config.pool = &sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 256))
		},
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			if config.Skipper(c) {
				return next(c)
			}

			req := c.Request()
			res := c.Response()
			start := time.Now()
			if err = next(c); err != nil {
				c.Error(err)
			}
			stop := time.Now()
			buf := config.pool.Get().(*bytes.Buffer)
			buf.Reset()
			defer config.pool.Put(buf)

			if _, err = config.template.ExecuteFunc(buf, func(w io.Writer, tag string) (int, error) {
				switch tag {
				case "custom":
					if config.CustomTagFunc == nil {
						return 0, nil
					}
					return config.CustomTagFunc(c, buf)
				case "time_unix":
					return buf.WriteString(strconv.FormatInt(timeNow().Unix(), 10))
				case "time_unix_milli":
					return buf.WriteString(strconv.FormatInt(timeNow().UnixMilli(), 10))
				case "time_unix_micro":
					return buf.WriteString(strconv.FormatInt(timeNow().UnixMicro(), 10))
				case "time_unix_nano":
					return buf.WriteString(strconv.FormatInt(timeNow().UnixNano(), 10))
				case "time_rfc3339":
					return buf.WriteString(timeNow().Format(time.RFC3339))
				case "time_rfc3339_nano":
					return buf.WriteString(timeNow().Format(time.RFC3339Nano))
				case "time_custom":
					return buf.WriteString(timeNow().Format(config.CustomTimeFormat))
				case "id":
					id := req.Header.Get(echo.HeaderXRequestID)
					if id == "" {
						id = res.Header().Get(echo.HeaderXRequestID)
					}
					return writeString(buf, id)
				case "remote_ip":
					return writeString(buf, c.RealIP())
				case "host":
					return writeString(buf, req.Host)
				case "uri":
					return writeString(buf, req.RequestURI)
				case "method":
					return writeString(buf, req.Method)
				case "path":
					p := req.URL.Path
					if p == "" {
						p = "/"
					}
					return writeString(buf, p)
				case "route":
					return writeString(buf, c.Path())
				case "protocol":
					return writeString(buf, req.Proto)
				case "referer":
					return writeString(buf, req.Referer())
				case "user_agent":
					return writeString(buf, req.UserAgent())
				case "status":
					n := res.Status
					s := config.colorer.Green(n)
					switch {
					case n >= 500:
						s = config.colorer.Red(n)
					case n >= 400:
						s = config.colorer.Yellow(n)
					case n >= 300:
						s = config.colorer.Cyan(n)
					}
					return buf.WriteString(s)
				case "error":
					if err != nil {
						return writeJSONSafeString(buf, err.Error())
					}
				case "latency":
					l := stop.Sub(start)
					return buf.WriteString(strconv.FormatInt(int64(l), 10))
				case "latency_human":
					return buf.WriteString(stop.Sub(start).String())
				case "bytes_in":
					cl := req.Header.Get(echo.HeaderContentLength)
					if cl == "" {
						cl = "0"
					}
					return writeString(buf, cl)
				case "bytes_out":
					return buf.WriteString(strconv.FormatInt(res.Size, 10))
				default:
					switch {
					case strings.HasPrefix(tag, "header:"):
						return writeString(buf, c.Request().Header.Get(tag[7:]))
					case strings.HasPrefix(tag, "query:"):
						return writeString(buf, c.QueryParam(tag[6:]))
					case strings.HasPrefix(tag, "form:"):
						return writeString(buf, c.FormValue(tag[5:]))
					case strings.HasPrefix(tag, "cookie:"):
						cookie, err := c.Cookie(tag[7:])
						if err == nil {
							return buf.Write([]byte(cookie.Value))
						}
					}
				}
				return 0, nil
			}); err != nil {
				return
			}

			if config.Output == nil {
				_, err = c.Logger().Output().Write(buf.Bytes())
				return
			}
			_, err = config.Output.Write(buf.Bytes())
			return
		}
	}
}
