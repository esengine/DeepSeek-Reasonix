package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"reasonix/internal/runtime/promptrefine"
)

// refinePrompt answers a draft rewritten by the session's model. Only the
// draft travels; the kernel reads the recent turns itself.
func (s *Server) refinePrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Draft string `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*promptrefine.MaxDraftBytes)).Decode(&body); err != nil {
		refuse(w, http.StatusBadRequest, "prompt_refine.bad_request", "the request body is not a JSON draft", nil)
		return
	}
	text, err := s.ctl().RefinePrompt(r.Context(), body.Draft)
	switch {
	case err == nil:
		writeJSON(w, map[string]string{"text": text})
	case r.Context().Err() != nil:
		// The person cancelled; nobody is left to read an answer.
	case errors.Is(err, promptrefine.ErrEmpty):
		refuse(w, http.StatusBadRequest, "prompt_refine.empty", err.Error(), nil)
	case errors.Is(err, promptrefine.ErrTooLong):
		refuse(w, http.StatusRequestEntityTooLarge, "prompt_refine.too_long", err.Error(), map[string]any{"max_bytes": promptrefine.MaxDraftBytes})
	case errors.Is(err, promptrefine.ErrUnavailable):
		refuse(w, http.StatusConflict, "prompt_refine.no_model", err.Error(), nil)
	case errors.Is(err, context.DeadlineExceeded):
		refuse(w, http.StatusGatewayTimeout, "prompt_refine.timeout", err.Error(), nil)
	case errors.Is(err, promptrefine.ErrNoAnswer):
		refuse(w, http.StatusBadGateway, "prompt_refine.no_answer", err.Error(), nil)
	default:
		refuse(w, http.StatusBadGateway, "prompt_refine.failed", err.Error(), nil)
	}
}
