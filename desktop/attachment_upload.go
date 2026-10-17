package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

const attachmentUploadPath = "/__reasonix_attachment_upload"

func (a *App) attachmentUploadMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != attachmentUploadPath {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				http.Error(w, "unsupported upload type", http.StatusUnsupportedMediaType)
				return
			}
			target, err := a.attachmentTargetToken(r.URL.Query().Get("target"))
			if err != nil {
				http.Error(w, "attachment target is unavailable", http.StatusConflict)
				return
			}
			limit, err := config.LoadAttachmentStreamLimit()
			if err != nil {
				http.Error(w, "attachment upload configuration could not be read", http.StatusInternalServerError)
				return
			}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			stop := context.AfterFunc(a.attachmentOperationContext(target), func() { cancel(); _ = r.Body.Close() })
			defer stop()
			rel, err := control.SaveAttachmentStreamInRoot(ctx, target.root, r.URL.Query().Get("name"), r.Body, limit)
			rel, err = a.finishAttachmentWrite(target, rel, err)
			if err != nil {
				if !a.attachmentTargetCurrent(target) {
					http.Error(w, "attachment upload could not be saved", http.StatusConflict)
					return
				}
				var limitErr *control.AttachmentStreamLimitError
				if errors.As(err, &limitErr) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					_ = json.NewEncoder(w).Encode(struct {
						Code        string `json:"code"`
						Message     string `json:"message"`
						LimitBytes  int64  `json:"limit_bytes"`
						LimitSource string `json:"limit_source"`
					}{"attachment.too_large", limitErr.Error(), limitErr.LimitBytes, limitErr.LimitSource})
					return
				}
				http.Error(w, "attachment upload could not be saved", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Path string `json:"path"`
			}{Path: rel})
		})
	}
}
