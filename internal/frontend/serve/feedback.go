// feedback.go — the report form's endpoints. The kernel owns validation,
// redaction, identity and the wire to the service; this file only speaks HTTP.
package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/surface"
	"reasonix/internal/platform/feedback"
)

// uploadCeiling bounds one submit body: the image cap in base64 plus the text.
const uploadCeiling = 16 << 20

const (
	codeFeedbackInvalid      = "feedback.invalid"
	codeFeedbackTooLarge     = "feedback.too_large"
	codeFeedbackRateLimited  = "feedback.rate_limited"
	codeFeedbackDisabled     = "feedback.disabled"
	codeFeedbackDuplicate    = "feedback.duplicate"
	codeFeedbackBadToken     = "feedback.bad_token"
	codeFeedbackOffline      = "feedback.offline"
	codeFeedbackUnavailable  = "feedback.unavailable"
	codeFeedbackInternal     = "feedback.internal"
	codeFeedbackBusy         = "feedback.busy"
	codeFeedbackImageMeta    = "feedback.image_metadata"
	codeFeedbackReplyLimit   = "feedback.reply_limit"
	codeFeedbackNotReplyable = "feedback.not_replyable"
	codeFeedbackChallenge    = "feedback.challenge_required"
)

// feedbackSurface names where a report from a hub or pane of surface from
// comes from. Studio and its desktop shell own the form; the terminal client
// owns /feedback; a plain serve has neither.
func feedbackSurface(from surface.Surface) feedback.Surface {
	switch from {
	case surface.Studio, surface.Desktop:
		return feedback.SurfaceStudio
	case surface.CLI:
		return feedback.SurfaceTUI
	}
	return ""
}

func (s *Server) registerFeedbackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /feedback/env", s.feedbackEnv)
	mux.HandleFunc("GET /feedback/mine", s.feedbackMine)
	mux.HandleFunc("POST /feedback", s.feedbackSubmit)
	mux.HandleFunc("POST /feedback/name", s.feedbackName)
	mux.HandleFunc("POST /feedback/{receipt}/reply", s.feedbackReply)
	mux.HandleFunc("POST /feedback/{receipt}/seen", s.feedbackSeen)
}

func (s *Server) feedbackEnv(w http.ResponseWriter, r *http.Request) {
	c := s.ctl()
	writeJSON(w, feedbackEnvView{
		Env:         c.FeedbackEnv(feedback.SurfaceStudio, r.URL.Query().Get("locale")),
		DisplayName: c.FeedbackDisplayName(),
		Limits:      c.FeedbackLimits(),
	})
}

type feedbackEnvView struct {
	Env         feedback.Env    `json:"env"`
	DisplayName string          `json:"displayName"`
	Limits      feedback.Limits `json:"limits"`
}

type feedbackImageBody struct {
	Name       string `json:"name"`
	DataBase64 string `json:"dataBase64"`
}

type feedbackSubmitBody struct {
	IdempotencyKey string              `json:"idempotencyKey"`
	Category       feedback.Category   `json:"category"`
	Body           string              `json:"body"`
	DisplayName    string              `json:"displayName"`
	Contact        string              `json:"contact"`
	Locale         string              `json:"locale"`
	TurnstileToken string              `json:"turnstileToken,omitempty"`
	Images         []feedbackImageBody `json:"images"`
}

func (s *Server) feedbackSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, uploadCeiling)
	var req feedbackSubmitBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuseFeedback(w, feedback.ErrTooLarge)
			return
		}
		badBody(w)
		return
	}
	d := feedback.Draft{
		IdempotencyKey: req.IdempotencyKey, Category: req.Category, Body: req.Body,
		DisplayName: req.DisplayName, Contact: req.Contact, TurnstileToken: req.TurnstileToken,
		Env: feedback.EnvContext{Surface: feedback.SurfaceStudio, Locale: req.Locale},
	}
	for _, img := range req.Images {
		raw, err := base64.StdEncoding.DecodeString(img.DataBase64)
		if err != nil {
			refuseFeedback(w, &feedback.InvalidError{Field: feedback.FieldImages, Reason: feedback.ReasonUndecodable})
			return
		}
		d.Images = append(d.Images, feedback.Image{Name: img.Name, Data: raw})
	}
	got, err := s.ctl().SubmitFeedback(r.Context(), d)
	if err != nil {
		refuseFeedback(w, err)
		return
	}
	writeJSON(w, got)
}

func (s *Server) feedbackMine(w http.ResponseWriter, r *http.Request) {
	got, err := s.ctl().ListFeedback(r.Context())
	if err != nil {
		refuseFeedback(w, err)
		return
	}
	writeJSON(w, got)
}

type feedbackReplyBody struct {
	Body string `json:"body"`
}

func (s *Server) feedbackReply(w http.ResponseWriter, r *http.Request) {
	var req feedbackReplyBody
	if !decodeBody(w, r, &req) {
		return
	}
	got, err := s.ctl().ReplyFeedback(r.Context(), r.PathValue("receipt"), req.Body)
	if err != nil {
		refuseFeedback(w, err)
		return
	}
	writeJSON(w, got)
}

type feedbackSeenBody struct {
	UpTo feedback.ReplyID `json:"upTo"`
}

func (s *Server) feedbackSeen(w http.ResponseWriter, r *http.Request) {
	var req feedbackSeenBody
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.ctl().MarkFeedbackSeen(r.PathValue("receipt"), req.UpTo); err != nil {
		refuseFeedback(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) feedbackName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"displayName"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.ctl().SetFeedbackDisplayName(req.DisplayName); err != nil {
		refuseFeedback(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refuseFeedback gives each identity the kernel reports its own code. The
// unreachable-service and unexpected-answer codes are separate from the domain
// ones because the next step differs: retry, versus change the request.
func refuseFeedback(w http.ResponseWriter, err error) {
	var invalid *feedback.InvalidError
	switch {
	case errors.As(err, &invalid):
		refuse(w, http.StatusBadRequest, codeFeedbackInvalid, err.Error(),
			map[string]any{"field": invalid.Field, "reason": invalid.Reason})
	case errors.Is(err, feedback.ErrInvalid):
		refuse(w, http.StatusBadRequest, codeFeedbackInvalid, err.Error(), nil)
	case errors.Is(err, feedback.ErrTooLarge):
		refuse(w, http.StatusRequestEntityTooLarge, codeFeedbackTooLarge, err.Error(), nil)
	case errors.Is(err, feedback.ErrRateLimited):
		params := map[string]any(nil)
		if after := feedback.RetryAfter(err); after > 0 {
			params = map[string]any{"retryAfterSeconds": int(after.Seconds())}
		}
		refuse(w, http.StatusTooManyRequests, codeFeedbackRateLimited, err.Error(), params)
	case errors.Is(err, feedback.ErrDisabled):
		refuse(w, http.StatusServiceUnavailable, codeFeedbackDisabled, err.Error(), nil)
	case errors.Is(err, feedback.ErrDuplicate):
		refuse(w, http.StatusConflict, codeFeedbackDuplicate, err.Error(), nil)
	case errors.Is(err, feedback.ErrBadToken):
		refuse(w, http.StatusConflict, codeFeedbackBadToken, err.Error(), nil)
	case errors.Is(err, feedback.ErrBusy):
		refuse(w, http.StatusServiceUnavailable, codeFeedbackBusy, err.Error(), nil)
	case errors.Is(err, feedback.ErrReplyLimit):
		refuse(w, http.StatusTooManyRequests, codeFeedbackReplyLimit, err.Error(), nil)
	case errors.Is(err, feedback.ErrNotReplyable):
		refuse(w, http.StatusConflict, codeFeedbackNotReplyable, err.Error(), nil)
	case errors.Is(err, feedback.ErrChallengeRequired):
		refuse(w, http.StatusForbidden, codeFeedbackChallenge, err.Error(), nil)
	case errors.Is(err, feedback.ErrImageMetadata):
		refuse(w, http.StatusBadRequest, codeFeedbackImageMeta, err.Error(), nil)
	case errors.Is(err, feedback.ErrOffline):
		refuse(w, http.StatusBadGateway, codeFeedbackOffline, err.Error(), nil)
	case errors.Is(err, feedback.ErrUnavailable):
		refuse(w, http.StatusBadGateway, codeFeedbackUnavailable, err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, codeFeedbackInternal, err.Error(), nil)
	}
}
