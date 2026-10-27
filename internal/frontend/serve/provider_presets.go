package serve

import (
	"fmt"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
)

// providerPresetView is one curated starting point as the gallery lists it:
// where it connects and over which protocol. The kernel owns the list, so a client never keeps
// a second copy that can drift from the entries the upgrade path recognises.
type providerPresetView struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl"`
	Vendor  string `json:"vendor"`
}

func (s *Server) providerPresets(w http.ResponseWriter, _ *http.Request) {
	out := []providerPresetView{}
	for _, preset := range config.CuratedProviderPresets() {
		if len(preset.Entries) == 0 {
			continue
		}
		entry := preset.Entries[0]
		out = append(out, providerPresetView{
			ID: preset.ID, Label: preset.Label, Kind: entry.Kind, BaseURL: entry.BaseURL, Vendor: preset.Vendor,
		})
	}
	writeJSON(w, out)
}

// installProviderPreset creates the entries a curated preset declares, whole:
// catalog, windows, vision models, thinking shape and the PresetID that the
// upgrade path and the adaptive-thinking and billing judgements read. The
// person supplies only a name and a key.
func (s *Server) installProviderPreset(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		PresetID string `json:"presetId"`
		Name     string `json:"name"`
		APIKey   string `json:"apiKey"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	preset, ok := config.CuratedProviderPreset(body.PresetID)
	if !ok {
		refuse(w, http.StatusNotFound, "provider.preset_unknown", "no curated preset has this id", map[string]any{"presetId": body.PresetID})
		return
	}
	known, err := config.Load()
	if err != nil || known == nil {
		refuse(w, http.StatusInternalServerError, "provider.config_unreadable", "the configuration could not be read, so no key slot can be chosen", nil)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name != "" && !providerNameRE.MatchString(name) {
		refuse(w, http.StatusBadRequest, "provider.name_invalid", "the name is not usable as a provider name", nil)
		return
	}
	if name != "" && len(preset.Entries) > 1 {
		refuse(w, http.StatusBadRequest, "provider.preset_named_entries", "a preset that creates several entries keeps its own names", nil)
		return
	}
	if name != "" {
		if _, taken := known.Provider(name); taken {
			refuse(w, http.StatusConflict, "provider.name_taken", "a provider with this name already exists", map[string]any{"name": name})
			return
		}
		preset.Entries[0].Name = name
	} else {
		for i := range preset.Entries {
			preset.Entries[i].Name = uniquePresetName(known, preset.Entries[i].Name)
		}
	}
	created := make([]string, 0, len(preset.Entries))
	for _, entry := range preset.Entries {
		created = append(created, entry.Name)
	}
	if !storeProviderEntries(w, known, preset.Entries, body.APIKey, false) {
		return
	}
	if err := s.leaveNoProvider(r.Context(), preset.Entries...); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"names": created})
}

func uniquePresetName(known *config.Config, base string) string {
	name := base
	for n := 2; ; n++ {
		if _, taken := known.Provider(name); !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
}
