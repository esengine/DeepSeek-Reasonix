package agent

import (
	"encoding/json"
)

const ()

func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
