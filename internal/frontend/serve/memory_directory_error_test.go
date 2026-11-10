package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/memory"
)

func TestMemoryEndpointsReportDirectoryReadFailure(t *testing.T) {
	for _, history := range []bool{false, true} {
		name := "active"
		if history {
			name = "history"
		}
		t.Run(name, func(t *testing.T) {
			store := memory.Store{Dir: testenv.TempDir(t)}
			first, err := store.SaveWithOptions(memory.Memory{Name: "fact", Body: "v1"}, memory.SaveOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Save(memory.Memory{Name: "fact", Body: "v2"}); err != nil {
				t.Fatal(err)
			}
			broken := store.Dir
			if history {
				broken = filepath.Join(store.Dir, ".revisions", first.Memory.ID)
			}
			if err := os.RemoveAll(broken); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(broken, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			c := control.New(control.Options{Memory: &memory.Set{Store: store}})
			t.Cleanup(c.Close)
			s := &Server{ctrl: c}
			type endpointTest struct {
				name      string
				call      func(http.ResponseWriter, *http.Request)
				url, body string
			}
			tests := []endpointTest{
				{"revisions", s.memoryRevisions, "/memory/revisions?name=fact", ""},
				{"restore", s.restoreMemory, "/memory/restore", `{"name":"fact","revision":1}`},
			}
			if !history {
				tests = append(tests,
					endpointTest{"list", s.memories, "/memories", ""},
					endpointTest{"save", s.saveMemory, "/memory/save", `{"name":"fact","body":"v3"}`},
					endpointTest{"forget", s.forgetMemory, "/memory/forget", `{"name":"fact"}`})
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					rec := httptest.NewRecorder()
					tc.call(rec, httptest.NewRequest(http.MethodPost, tc.url, strings.NewReader(tc.body)))
					var reason Reason
					if err := json.Unmarshal(rec.Body.Bytes(), &reason); err != nil {
						t.Fatal(err)
					}
					if rec.Code != http.StatusInternalServerError || reason.Code != "memory.directory_unreadable" {
						t.Fatalf("response = %d %s", rec.Code, rec.Body)
					}
				})
			}
		})
	}
}
