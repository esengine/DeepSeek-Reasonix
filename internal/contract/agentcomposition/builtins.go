package agentcomposition

import (
	_ "embed"
	"encoding/json"
)

//go:embed builtins.json
var builtinData []byte

func Builtins() []Definition {
	var out []Definition
	_ = json.Unmarshal(builtinData, &out)
	return out
}
