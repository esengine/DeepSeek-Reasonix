// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/bytes"
)

// StaticConfig defines the config for Static middleware.
type StaticConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Root directory from where the static content is served.
	// Required.
	Root string `yaml:"root"`

	// Index file for serving a directory.
	// Optional. Default value "index.html".
	Index string `yaml:"index"`

	// Enable HTML5 mode by forwarding all not-found requests to root so that
	// SPA (single-page application) can handle the routing.
	// Optional. Default value false.
	HTML5 bool `yaml:"html5"`

	// Enable directory browsing.
	// Optional. Default value false.
	Browse bool `yaml:"browse"`

	// Enable ignoring of the base of the URL path.
	// Example: when assigning a static middleware to a non root path group,
	// the filesystem path is not doubled
	// Optional. Default value false.
	IgnoreBase bool `yaml:"ignoreBase"`

	// Filesystem provides access to the static content.
	// Optional. Defaults to http.Dir(config.Root)
	Filesystem http.FileSystem `yaml:"-"`
}

const html = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="X-UA-Compatible" content="ie=edge">
  <title>{{ .Name }}</title>
  <style>
    body {
			font-family: Menlo, Consolas, monospace;
			padding: 48px;
		}
		header {
			padding: 4px 16px;
			font-size: 24px;
		}
    ul {
			list-style-type: none;
			margin: 0;
    	padding: 20px 0 0 0;
			display: flex;
			flex-wrap: wrap;
    }
    li {
			width: 300px;
			padding: 16px;
		}
		li a {
			display: block;
			overflow: hidden;
			white-space: nowrap;
			text-overflow: ellipsis;
			text-decoration: none;
			transition: opacity 0.25s;
		}
		li span {
			color: #707070;
			font-size: 12px;
		}
		li a:hover {
			opacity: 0.50;
		}
		.dir {
			color: #E91E63;
		}
		.file {
			color: #673AB7;
		}
  </style>
</head>
<body>
	<header>
		{{ .Name }}
	</header>
	<ul>
		{{ range .Files }}
		<li>
		{{ if .Dir }}
			{{ $name := print .Name "/" }}
			<a class="dir" href="{{ $name }}">{{ $name }}</a>
			{{ else }}
			<a class="file" href="{{ .Name }}">{{ .Name }}</a>
			<span>{{ .Size }}</span>
		{{ end }}
		</li>
		{{ end }}
  </ul>
</body>
</html>
`

// DefaultStaticConfig is the default Static middleware config.
var DefaultStaticConfig = StaticConfig{
	Skipper: DefaultSkipper,
	Index:   "index.html",
}

// Static returns a Static middleware to serves static content from the provided
// root directory.
func Static(root string) echo.MiddlewareFunc {
	c := DefaultStaticConfig
	c.Root = root
	return StaticWithConfig(c)
}

// StaticWithConfig returns a Static middleware with config.
// See `Static()`.
func StaticWithConfig(config StaticConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Root == "" {
		config.Root = "." // For security we want to restrict to CWD.
	}
	if config.Skipper == nil {
		config.Skipper = DefaultStaticConfig.Skipper
	}
	if config.Index == "" {
		config.Index = DefaultStaticConfig.Index
	}
	if config.Filesystem == nil {
		config.Filesystem = http.Dir(config.Root)
		config.Root = "."
	}

	// Index template
	t, tErr := template.New("index").Parse(html)
	if tErr != nil {
		panic(fmt.Errorf("echo: %w", tErr))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			if config.Skipper(c) {
				return next(c)
			}

			p := c.Request().URL.Path
			if strings.HasSuffix(c.Path(), "*") { // When serving from a group, e.g. `/static*`.
				p = c.Param("*")
			}
			p, err = url.PathUnescape(p)
			if err != nil {
				return
			}
			// Security: We use path.Clean() (not filepath.Clean()) because:
			// 1. HTTP URLs always use forward slashes, regardless of server OS
			// 2. path.Clean() provides platform-independent behavior for URL paths
			// 3. The "/" prefix forces absolute path interpretation, removing ".." components
			// 4. Backslashes are treated as literal characters (not path separators), preventing traversal
			// See static_windows.go for Go 1.20+ filepath.Clean compatibility notes
			name := path.Join(config.Root, path.Clean("/"+p)) // "/"+ for security

			if config.IgnoreBase {
				routePath := path.Base(strings.TrimRight(c.Path(), "/*"))
				baseURLPath := path.Base(p)
				if baseURLPath == routePath {
					i := strings.LastIndex(name, routePath)
					name = name[:i] + strings.Replace(name[i:], routePath, "", 1)
				}
			}

			file, err := config.Filesystem.Open(name)
			if err != nil {
				if !isIgnorableOpenFileError(err) {
					return err
				}

				// file with that path did not exist, so we continue down in middleware/handler chain, hoping that we end up in
				// handler that is meant to handle this request
				if err = next(c); err == nil {
					return err
				}

				var he *echo.HTTPError
				if !(errors.As(err, &he) && config.HTML5 && he.Code == http.StatusNotFound) {
					return err
				}

				file, err = config.Filesystem.Open(path.Join(config.Root, config.Index))
				if err != nil {
					return err
				}
			}

			defer file.Close()

			info, err := file.Stat()
			if err != nil {
				return err
			}

			if info.IsDir() {
				index, err := config.Filesystem.Open(path.Join(name, config.Index))
				if err != nil {
					if config.Browse {
						return listDir(t, name, file, c.Response())
					}

					return next(c)
				}

				defer index.Close()

				info, err = index.Stat()
				if err != nil {
					return err
				}

				return serveFile(c, index, info)
			}

			return serveFile(c, file, info)
		}
	}
}

func serveFile(c echo.Context, file http.File, info os.FileInfo) error {
	http.ServeContent(c.Response(), c.Request(), info.Name(), info.ModTime(), file)
	return nil
}

func listDir(t *template.Template, name string, dir http.File, res *echo.Response) (err error) {
	files, err := dir.Readdir(-1)
	if err != nil {
		return
	}

	// Create directory index
	res.Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	data := struct {
		Name  string
		Files []interface{}
	}{
		Name: name,
	}
	for _, f := range files {
		data.Files = append(data.Files, struct {
			Name string
			Dir  bool
			Size string
		}{f.Name(), f.IsDir(), bytes.Format(f.Size())})
	}
	return t.Execute(res, data)
}
