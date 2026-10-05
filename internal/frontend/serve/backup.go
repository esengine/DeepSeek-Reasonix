// backup.go — configuration backups kept in the signed-in account. The kernel
// seals and opens them; the accounts service only ever holds the envelope.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"reasonix/internal/ext/configbackup"
	"reasonix/internal/platform/account"
)

// One planner per process: the machine being restored is one machine, however
// many panes are looking at it.
var backupPlanner = configbackup.NewPlanner()

const maxBackupLabelRunes = 80

func (s *Server) registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /backups", s.listBackups)
	mux.HandleFunc("POST /backups", s.createBackup)
	mux.HandleFunc("DELETE /backups/{id}", s.deleteBackup)
	mux.HandleFunc("POST /backups/{id}/preview", s.previewBackup)
	mux.HandleFunc("POST /backups/apply", s.applyBackup)
}

// backupSession is the gate every backup route shares: the host must allow
// account routes and someone must be signed in. There is no local-only mode.
func (s *Server) backupSession(w http.ResponseWriter, r *http.Request) (*account.Client, string, bool) {
	if !s.accountAuthAllowed(w, r) {
		return nil, "", false
	}
	token := account.Token()
	if token == "" {
		refuse(w, http.StatusUnauthorized, "backup.signed_out", "sign in to use backups", nil)
		return nil, "", false
	}
	client, err := s.accountClient()
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "backup.cloud_unavailable", err)
		return nil, "", false
	}
	return client, token, true
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	client, token, ok := s.backupSession(w, r)
	if !ok {
		return
	}
	backups, limits, err := client.ListBackups(r.Context(), token)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	if backups == nil {
		backups = []account.Backup{}
	}
	writeJSON(w, map[string]any{
		"categories":    configbackup.Categories(),
		"backups":       backups,
		"limits":        limits,
		"minPassphrase": configbackup.MinPassphraseRunes,
	})
}

type createBackupRequest struct {
	Label      string   `json:"label"`
	Categories []string `json:"categories"`
	Passphrase string   `json:"passphrase"`
	AppVersion string   `json:"appVersion"`
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	client, token, ok := s.backupSession(w, r)
	if !ok {
		return
	}
	var req createBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	label := strings.TrimSpace(req.Label)
	if utf8.RuneCountInString(label) > maxBackupLabelRunes {
		refuse(w, http.StatusBadRequest, "backup.label_too_long", "the label is too long",
			map[string]any{"max": maxBackupLabelRunes})
		return
	}
	cats, err := configbackup.ParseCategories(req.Categories)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	snap, err := configbackup.Collect(configbackup.CollectOptions{Categories: cats, AppVersion: strings.TrimSpace(req.AppVersion)})
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "backup.collect_failed", err)
		return
	}
	envelope, err := configbackup.Seal(snap, req.Passphrase)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = string(c)
	}
	meta := account.BackupUpload{
		Label: label, Format: snap.Format, AppVersion: snap.AppVersion, Platform: snap.Platform, Categories: names,
	}
	backup, err := client.UploadBackup(r.Context(), token, meta, envelope)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	writeJSON(w, map[string]any{"backup": backup, "omitted": snap.Omitted})
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	client, token, ok := s.backupSession(w, r)
	if !ok {
		return
	}
	if err := client.DeleteBackup(r.Context(), token, r.PathValue("id")); err != nil {
		refuseBackup(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) previewBackup(w http.ResponseWriter, r *http.Request) {
	client, token, ok := s.backupSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	_, envelope, err := client.DownloadBackup(r.Context(), token, r.PathValue("id"))
	if err != nil {
		refuseBackup(w, err)
		return
	}
	snap, err := configbackup.Open(envelope, req.Passphrase)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	plan, err := backupPlanner.Preview(snap)
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "backup.preview_failed", err)
		return
	}
	writeJSON(w, plan)
}

// applyBackup writes what the person chose. Consent is checked by the kernel
// against the machine as it is now; this handler only carries the answer.
func (s *Server) applyBackup(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.backupSession(w, r); !ok {
		return
	}
	var req configbackup.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		missingField(w, "planId")
		return
	}
	res, err := backupPlanner.Apply(req)
	if err != nil {
		refuseBackup(w, err)
		return
	}
	out := map[string]any{"applied": res.Applied, "plugins": res.Plugins, "failed": res.Failed}
	if len(res.Applied) > 0 {
		if err := s.reloadExtensions(r.Context()); err != nil {
			out["reloadError"] = err.Error()
		}
	}
	writeJSON(w, out)
}

// refuseBackup projects the class the kernel and the account client already
// decided. Anything neither names is the accounts service failing, which is a
// dependency answer, never a claim that the request was wrong.
func refuseBackup(w http.ResponseWriter, err error) {
	var consent *configbackup.ConsentError
	switch {
	case errors.As(err, &consent):
		refuse(w, http.StatusConflict, "backup.consent_required", err.Error(), map[string]any{"ids": consent.IDs})
	case errors.Is(err, account.ErrUnauthorized):
		refuse(w, http.StatusUnauthorized, "backup.signed_out", "sign in to use backups", nil)
	case errors.Is(err, configbackup.ErrWeakPassphrase):
		refuse(w, http.StatusBadRequest, "backup.weak_passphrase", err.Error(),
			map[string]any{"min": configbackup.MinPassphraseRunes})
	case errors.Is(err, configbackup.ErrNoCategories), errors.Is(err, configbackup.ErrUnknownCategory):
		refuse(w, http.StatusBadRequest, "backup.bad_categories", err.Error(), nil)
	case errors.Is(err, configbackup.ErrCannotDecrypt):
		refuse(w, http.StatusUnprocessableEntity, "backup.cannot_decrypt", err.Error(), nil)
	case errors.Is(err, configbackup.ErrUnsupportedFormat):
		refuse(w, http.StatusUnprocessableEntity, "backup.unsupported_format", err.Error(), nil)
	case errors.Is(err, configbackup.ErrMalformed):
		refuse(w, http.StatusUnprocessableEntity, "backup.malformed", err.Error(), nil)
	case errors.Is(err, configbackup.ErrTooLarge), errors.Is(err, account.ErrBackupTooLarge):
		refuse(w, http.StatusRequestEntityTooLarge, "backup.too_large", err.Error(), nil)
	case errors.Is(err, configbackup.ErrPlanExpired):
		refuse(w, http.StatusGone, "backup.plan_expired", err.Error(), nil)
	case errors.Is(err, configbackup.ErrUnknownItem):
		refuse(w, http.StatusBadRequest, "backup.unknown_item", err.Error(), nil)
	case errors.Is(err, account.ErrBackupNotFound):
		refuse(w, http.StatusNotFound, "backup.not_found", err.Error(), nil)
	case errors.Is(err, account.ErrBackupLimit):
		refuse(w, http.StatusConflict, "backup.limit_reached", err.Error(), nil)
	default:
		refuse(w, http.StatusBadGateway, "backup.cloud_unavailable", err.Error(), map[string]any{"detail": err.Error()})
	}
}
