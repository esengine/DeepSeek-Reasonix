package echo

import (
	"github.com/stretchr/testify/assert"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderWithTemplateRenderer(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)

	e.Renderer = &TemplateRenderer{
		Template: template.Must(template.New("hello").Parse("Hello, {{.}}!")),
	}

	err := c.Render(http.StatusOK, "hello", "Jon Snow")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Hello, Jon Snow!", rec.Body.String())
	}
}
