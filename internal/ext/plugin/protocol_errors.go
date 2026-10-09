package plugin

import (
	"encoding/json"
	"fmt"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d (message omitted; %d bytes)", e.Code, len(e.Message))
}

func (e *rpcError) DiagnosticFacts() string { return e.Error() }
