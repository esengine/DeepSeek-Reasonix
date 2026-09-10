package main

import (
	_ "embed"
	"encoding/json"

	"reasonix/desktop/internal/hostrpc"
)

const hostCommandOwnersFile = "host_command_owners.generated.json"

//go:embed host_command_owners.generated.json
var hostCommandOwnersJSON []byte

func newDesktopRegistry(app *App) (*hostrpc.Registry, error) {
	var owners map[string]hostrpc.CommandOwnership
	if err := json.Unmarshal(hostCommandOwnersJSON, &owners); err != nil {
		return nil, err
	}
	return hostrpc.NewRegistryWithOwners(app, nil, owners)
}
