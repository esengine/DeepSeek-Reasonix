package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestModelsDecodesCatalogEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []ModelChoice
	}{
		{
			name: "choices",
			body: `{"current":"fixture/chat","label":"chat","default":"fixture/chat","models":[{"ref":"fixture/chat","provider":"fixture","model":"chat","answers":"text","active":true}]}`,
			want: []ModelChoice{{Ref: "fixture/chat", Provider: "fixture", Model: "chat", Answers: "text", Active: true}},
		},
		{name: "empty", body: `{"current":"","label":"","default":"","models":[]}`, want: []ModelChoice{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/models" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := &Client{HTTP: srv.Client(), Base: srv.URL}
			got, err := c.Models(context.Background())
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Models() = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
