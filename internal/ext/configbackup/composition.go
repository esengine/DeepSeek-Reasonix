package configbackup

import (
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/agentpreset"
)

const KindAgentPreset = "agent_preset"

type compositionData struct {
	Definition agentcomposition.Definition `json:"definition"`
	Default    bool                        `json:"default,omitempty"`
}

func (c *collector) compositions() error {
	catalog, err := agentpreset.New(config.ReasonixHomeDir(), "").List()
	if err != nil {
		return err
	}
	for _, v := range catalog.Presets {
		if strings.HasPrefix(v.Definition.ID, "user:") {
			if err := c.add(KindAgentPreset, v.Definition.ID, compositionData{v.Definition, v.Definition.ID == catalog.Default}); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeCompositionItem(it Item) (compositionData, error) {
	var data compositionData
	if err := json.Unmarshal(it.Data, &data); err != nil {
		return data, err
	}
	if !strings.HasPrefix(it.Name, "user:") || it.Name != data.Definition.ID {
		return data, ErrMalformed
	}
	return data, data.Definition.Validate()
}

func restoreCompositionItem(it Item) error {
	data, err := decodeCompositionItem(it)
	if err != nil {
		return err
	}
	store := agentpreset.New(config.ReasonixHomeDir(), "")
	old, err := store.Get(it.Name)
	if err != nil && !errors.Is(err, agentpreset.ErrNotFound) {
		return err
	}
	if _, err := store.Save(data.Definition, old.Revision); err != nil {
		return err
	}
	if data.Default {
		return store.SetDefault(it.Name, false)
	}
	return nil
}
