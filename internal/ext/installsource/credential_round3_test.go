package installsource

import (
	"errors"
	"strings"
	"testing"
)

func TestRound3InstallDiagnosticsOmitUnstructuredText(t *testing.T) {
	err := newErr(ErrSourceUnreadable, "subprocess: arbitrary neutralprobe")
	if !errors.Is(err, ErrSourceUnreadable) {
		t.Fatal("failure identity lost")
	}
	for _, got := range []string{err.Error(), marshalJSON(publicActions([]action{{Error: "Basic rxprobe"}})), marshalJSON(response{Error: "Bearer rxprobe"})} {
		if strings.Contains(got, "neutralprobe") || strings.Contains(got, "rxprobe") {
			t.Errorf("unstructured diagnostic survived: %s", got)
		}
	}
}
