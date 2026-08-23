package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadUserDataFileOpenAI(t *testing.T) {
	var gotPurpose, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		gotPurpose = r.FormValue("purpose")
		file, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		raw, _ := io.ReadAll(file)
		if hdr.Filename != "shot.png" || string(raw) != "PNGDATA" {
			t.Errorf("file = %q %q", hdr.Filename, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-api-0a1b2c3d4e5f6071","object":"file"}`))
	}))
	defer srv.Close()

	id, err := UploadUserDataFile(context.Background(), FileUpload{
		BaseURL:  srv.URL,
		APIKey:   "sk-test",
		Filename: "shot.png",
		Data:     []byte("PNGDATA"),
		Client:   srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "file-api-0a1b2c3d4e5f6071" {
		t.Fatalf("id = %q", id)
	}
	if gotPurpose != "user_data" || gotAuth != "Bearer sk-test" {
		t.Fatalf("purpose=%q auth=%q", gotPurpose, gotAuth)
	}
}

func TestUploadUserDataFileAnthropic(t *testing.T) {
	var gotBeta, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			t.Errorf("path = %s, want /v1/files", r.URL.Path)
		}
		gotBeta = r.Header.Get("anthropic-beta")
		gotKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-api-anthropicfile01","type":"file"}`))
	}))
	defer srv.Close()

	id, err := UploadUserDataFile(context.Background(), FileUpload{
		BaseURL:  srv.URL,
		APIKey:   "sk-ant",
		Protocol: "anthropic",
		Filename: "a.jpg",
		Data:     []byte("JPEG"),
		Client:   srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "file-api-anthropicfile01" {
		t.Fatalf("id = %q", id)
	}
	if gotBeta != "files-api-2025-04-14" || gotKey != "sk-ant" {
		t.Fatalf("beta=%q key=%q", gotBeta, gotKey)
	}
}

func TestUploadUserDataFileRejectsEmpty(t *testing.T) {
	_, err := UploadUserDataFile(context.Background(), FileUpload{APIKey: "k", Data: nil})
	if err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("err = %v", err)
	}
}
