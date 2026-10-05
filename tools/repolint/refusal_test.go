package main

import "testing"

const refusalSource = `package p

import "net/http"

func h(w http.ResponseWriter) {
	http.Error(w, "no", http.StatusInternalServerError)
	refuse(w, http.StatusBadRequest, "a.b", "no", nil)
}
`

const errorBodySource = `package p

import "net/http"

func h(w http.ResponseWriter, err error) {
	writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	writeJSONStatus(w, http.StatusBadGateway, map[string]any{"name": "x", "error": err.Error()})
	writeJSON(w, map[string]string{"message": "ok"})
}
`

func TestRefusalPathFlagsPlainHTTPErrorInServe(t *testing.T) {
	s := parseBytes("internal/frontend/serve/x.go", []byte(refusalSource))
	got := checkRefusalPath(s)
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1: %v", len(got), got)
	}
	if got[0].Rule != ruleRefusalPath || got[0].Weight != 1 {
		t.Fatalf("a pass/fail rule must weigh one: %+v", got[0])
	}
}

// The rule is about the refusal contract one package owes its frontends, not
// about http.Error everywhere: a CLI or a worker has no Reason to send.
func TestRefusalPathIgnoresPackagesWithoutThatContract(t *testing.T) {
	for _, rel := range []string{"internal/frontend/cli/x.go", "internal/frontend/serve/x_test.go", "cmd/reasonix/main.go"} {
		if got := checkRefusalPath(parseBytes(rel, []byte(refusalSource))); len(got) != 0 {
			t.Fatalf("%s was flagged: %v", rel, got)
		}
	}
}

// The adapter is exempt because it is where the fallback lives; the exemption
// is one file, not a habit that spreads to whatever moves in beside it.
func TestRefusalPathExemptsOnlyTheAdapter(t *testing.T) {
	if got := checkRefusalPath(parseBytes(refusalAdapter, []byte(refusalSource))); len(got) != 0 {
		t.Fatalf("the adapter was flagged: %v", got)
	}
	for _, rel := range []string{"internal/frontend/serve/fail_extra.go", "internal/frontend/serve/failure.go", "internal/frontend/serve/sub/fail.go"} {
		if got := checkRefusalPath(parseBytes(rel, []byte(refusalSource))); len(got) != 1 {
			t.Fatalf("%s took the adapter's exemption: %v", rel, got)
		}
	}
}

// The envelope was never the point: a status and an English sentence read the
// same to a frontend whether they arrive as text or as JSON. A body carrying
// anything else is a report the panel renders, and stays out of it.
func TestRefusalPathFlagsAnErrorOnlyBody(t *testing.T) {
	got := checkRefusalPath(parseBytes("internal/frontend/serve/x.go", []byte(errorBodySource)))
	if len(got) != 1 {
		t.Fatalf("findings = %d, want only the error-only body: %v", len(got), got)
	}
	if got[0].Line != 6 {
		t.Fatalf("flagged line %d, want the error-only body on 6: %v", got[0].Line, got[0])
	}
}
