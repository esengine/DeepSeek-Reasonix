package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteSessionListingPreservesReadActivity(t *testing.T) {
	isolateDesktopUserDirs(t)
	for _, fields := range []string{`,"resultSequence":0,"metadataReady":true`, `,"resultSequence":12,"metadataReady":true`, `,"resultSequence":0,"metadataReady":false`, ``} {
		t.Run(fields, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`[{"sessionId":"a","name":"a","title":"A","turns":1,"mtimeMilli":123` + fields + `}]`))
			}))
			defer server.Close()
			a := &App{}
			rows, err := a.remoteProjectSessions(t.Context(), server.Client(), server.URL, "host", "/repo")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(rows)
			var got []map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("rows: %s", raw)
			}
			var want map[string]any
			_ = json.Unmarshal([]byte(`{"name":"a"`+fields+`}`), &want)
			for _, key := range []string{"resultSequence", "metadataReady"} {
				// false may be omitted, but missing resultSequence must stay missing.
				if key == "metadataReady" && want[key] != true {
					continue
				}
				if got[0][key] != want[key] {
					t.Fatalf("%s lost: %s", key, raw)
				}
			}
		})
	}
}
