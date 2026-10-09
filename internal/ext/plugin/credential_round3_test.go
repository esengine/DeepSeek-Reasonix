package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRound3RemoteBodiesOmitted(t *testing.T) {
	for _, body := range []string{"Basic rxprobe", "Bearer rxprobe", "arbitrary neutralprobe"} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			httpTr, err := newHTTPTransport(Spec{Name: "neutral", URL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer httpTr.close()
			_, httpErr := httpTr.call(t.Context(), "prompts/list", nil)
			sseTr, err := newSSETransport(t.Context(), Spec{Name: "neutral", URL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer sseTr.close()
			sseErr := sseTr.waitEndpoint(t.Context())
			for _, failure := range []error{httpErr, sseErr} {
				var status *httpStatusError
				if !errors.As(failure, &status) || status.Status != http.StatusInternalServerError {
					t.Fatalf("status lost: %v", failure)
				}
				if strings.Contains(failure.Error(), body) || strings.Contains(failure.Error(), "rxprobe") || strings.Contains(failure.Error(), "neutralprobe") {
					t.Errorf("remote body survived: %v", failure)
				}
			}
		})
	}
}

func TestRound3RPCAndStderrOmitted(t *testing.T) {
	rpc := &rpcError{Code: -32000, Message: "unstructured neutralprobe"}
	if strings.Contains(rpc.Error(), "neutralprobe") {
		t.Errorf("RPC prose survived: %s", rpc)
	}
	tr := &stdioTransport{stderr: &tailBuffer{limit: 1024}}
	_, _ = tr.stderr.Write([]byte("unstructured neutralprobe"))
	if got := tr.withStderr(errors.New("fixture exit")).Error(); strings.Contains(got, "neutralprobe") || !strings.Contains(got, fmt.Sprintf("%d bytes", len("unstructured neutralprobe"))) {
		t.Errorf("stderr facts = %s", got)
	}
}
