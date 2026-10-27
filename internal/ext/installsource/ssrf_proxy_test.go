package installsource

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestGuardRefusesAnInternalLiteralEvenThroughAProxy(t *testing.T) {
	var reached atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := ssrfGuardClient(&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}})
	for _, target := range []string{"http://169.254.169.254/latest/meta-data/", "http://10.0.0.5/"} {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s was forwarded through the proxy", target)
		}
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("the proxy saw %d requests for blocked addresses, want 0", n)
	}
}
