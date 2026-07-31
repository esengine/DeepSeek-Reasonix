### Issue Description

### Working code to debug

```go
package main

import (
  "github.com/labstack/echo/v4"
  "net/http"
  "net/http/httptest"
  "testing"
)

func TestExample(t *testing.T) {
  e := echo.New()

  e.GET("/", func(c echo.Context) error {
    return c.String(http.StatusOK, "Hello, World!")
  })

  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()

  e.ServeHTTP(rec, req)

  if rec.Code != http.StatusOK {
    t.Errorf("got %d, want %d", rec.Code, http.StatusOK)
  }
}
```

### Version/commit
