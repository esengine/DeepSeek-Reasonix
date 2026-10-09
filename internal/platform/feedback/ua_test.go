package feedback

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestFeedbackRequestsCarryTheClientIdentity(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	s := &Service{http: srv.Client()}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.do(req, nil)
	if got != provider.ClientUserAgent() {
		t.Fatalf("User-Agent = %q, want %q", got, provider.ClientUserAgent())
	}
}
