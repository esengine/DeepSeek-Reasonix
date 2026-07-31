package echo

import "io"

// Renderer is the interface that wraps the Render function.
type Renderer interface {
	Render(io.Writer, string, interface{}, Context) error
}

// TemplateRenderer is helper to ease creating renderers for `html/template` and `text/template` packages.
// Example usage:
//
//		e.Renderer = &echo.TemplateRenderer{
//			Template: template.Must(template.ParseGlob("templates/*.html")),
//		}
//
//	  e.Renderer = &echo.TemplateRenderer{
//			Template: template.Must(template.New("hello").Parse("Hello, {{.}}!")),
//		}
type TemplateRenderer struct {
	Template interface {
		ExecuteTemplate(wr io.Writer, name string, data any) error
	}
}

// Render renders the template with given data.
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c Context) error {
	return t.Template.ExecuteTemplate(w, name, data)
}
