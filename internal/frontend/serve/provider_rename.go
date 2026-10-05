// provider_rename.go — the label a person gives a source, apart from its name.
package serve

import (
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
)

// renameProvider sets the display name of every entry the panel shows as one
// account. The names travel together so the account is relabelled whole or not
// at all. Only the label changes: refs, the default model and sessions keep
// pointing at the name, so nothing is rebuilt.
func (s *Server) renameProvider(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Names       []string `json:"names"`
		DisplayName string   `json:"displayName"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	names := trimmedNonEmpty(body.Names)
	if len(names) == 0 {
		missingField(w, "names")
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	if err := edit.SetProviderDisplayName(names, body.DisplayName); err != nil {
		switch {
		case errors.Is(err, config.ErrProviderNotFound):
			notFound(w, "provider", strings.Join(names, ", "))
		case errors.Is(err, config.ErrProviderDisplayNameTooLong):
			refuse(w, http.StatusBadRequest, "provider.display_name_too_long", "the display name is too long",
				map[string]any{"max": config.MaxProviderDisplayNameRunes})
		case errors.Is(err, config.ErrProviderDisplayNameInvalid):
			refuse(w, http.StatusBadRequest, "provider.display_name_invalid", "the display name contains a control character", nil)
		default:
			writeErr(w, http.StatusInternalServerError, err)
		}
		return
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
