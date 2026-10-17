package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestAttachmentUploadEnforcesUserStreamLimitAndCleansPartialFile(t *testing.T) {
	for _, tc := range []struct {
		name, user, project, body string
		length                    int64
		status                    int
	}{
		{"exact", "4", "", "1234", 0, http.StatusOK},
		{"plus one", "4", "", "12345", 0, http.StatusRequestEntityTooLarge},
		{"unknown length", "4", "", "12345", -1, http.StatusRequestEntityTooLarge},
		{"misleading length", "4", "", "12345", 1, http.StatusRequestEntityTooLarge},
		{"project cannot disable", "4", "0", "12345", -1, http.StatusRequestEntityTooLarge},
		{"user unlimited", "0", "1", "123456", -1, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, root, token := uploadFixture(t)
			if err := os.WriteFile(config.UserConfigPath(), []byte("[attachments]\nmax_stream_bytes = "+tc.user+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.project != "" {
				if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[attachments]\nmax_stream_bytes = "+tc.project+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			req := uploadRequest(token, strings.NewReader(tc.body))
			if tc.length != 0 {
				req.ContentLength = tc.length
			}
			recorder := httptest.NewRecorder()
			app.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.status, recorder.Body)
			}
			if tc.status != http.StatusRequestEntityTooLarge {
				return
			}
			var result struct {
				Code        string `json:"code"`
				Message     string `json:"message"`
				LimitBytes  int64  `json:"limit_bytes"`
				LimitSource string `json:"limit_source"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Code != "attachment.too_large" || result.Message == "" || result.LimitBytes != 4 || result.LimitSource != "user" {
				t.Fatalf("untyped limit error: %+v", result)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial files=%v err=%v", entries, err)
			}
		})
	}
}

func TestAttachmentUploadRejectsUnreadableUserLimitBeforeCreatingFile(t *testing.T) {
	app, root, token := uploadFixture(t)
	if err := os.WriteFile(config.UserConfigPath(), []byte("[attachments]\nmax_stream_bytes = [broken"), 0600); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	app.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(recorder, uploadRequest(token, strings.NewReader("body")))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("configuration failure accepted upload: %d %s", recorder.Code, recorder.Body)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "attachments")); !os.IsNotExist(err) {
		t.Fatalf("configuration failure created attachment files: %v", err)
	}
}

type uploadLimitMutationReader struct {
	body   *strings.Reader
	mutate func()
}

func (reader *uploadLimitMutationReader) Read(p []byte) (int, error) {
	if reader.mutate != nil {
		reader.mutate()
		reader.mutate = nil
	}
	return reader.body.Read(p)
}

func TestAttachmentUploadRetiredOwnerWinsOverStreamLimit(t *testing.T) {
	app, root, token := uploadFixture(t)
	if err := os.WriteFile(config.UserConfigPath(), []byte("[attachments]\nmax_stream_bytes = 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &uploadLimitMutationReader{body: strings.NewReader("12345"), mutate: func() { app.tabs["a"].SessionGeneration++ }}
	recorder := httptest.NewRecorder()
	app.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(recorder, uploadRequest(token, reader))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("retired owner error=%d body=%s", recorder.Code, recorder.Body)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial files=%v err=%v", entries, err)
	}
}
