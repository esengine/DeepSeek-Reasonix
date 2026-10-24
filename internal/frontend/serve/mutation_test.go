package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/state/mutation"
)

func TestMutationRefusalPreservesDomainIdentity(t *testing.T) {
	for _, cause := range []error{
		&mutation.VersionConflict{Path: "changed.txt"},
		&mutation.WaitError{TransactionID: "transaction", Paths: []string{"waiting.txt"}, Cause: context.DeadlineExceeded},
	} {
		w := httptest.NewRecorder()
		if !refuseMutation(w, fmt.Errorf("operation: %w", cause)) || w.Code != http.StatusConflict {
			t.Fatalf("lost mutation refusal: %d %s", w.Code, w.Body.String())
		}
		var reason Reason
		if err := json.Unmarshal(w.Body.Bytes(), &reason); err != nil {
			t.Fatal(err)
		}
		identity := cause.(interface{ RefusalCode() string }).RefusalCode()
		if reason.Code != identity || len(reason.Params) == 0 {
			t.Fatalf("lost domain code or context: %+v", reason)
		}
	}
	w := httptest.NewRecorder()
	if refuseMutation(w, errors.New("dependency failure")) || w.Body.Len() != 0 {
		t.Fatal("classified an unrelated failure as contention")
	}
}
