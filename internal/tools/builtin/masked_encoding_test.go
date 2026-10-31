package builtin

import (
	"encoding/json"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
)

func TestEscapedSecretMarksCannotReachWritesOrCommands(t *testing.T) {
	secretstest.Hold("ENCODED_MARK", "encoded-mark-value-01234")
	args := json.RawMessage(`{"path":".env","content":"\u005bsecret:ENCODED_MARK]","command":"echo \u005bsecret:ENCODED_MARK]"}`)
	if err := refuseMaskedWrite(args); err == nil {
		t.Error("escaped mark bypassed the write refusal")
	}
	if err := refuseMaskedCommand(args); err == nil {
		t.Error("escaped mark bypassed the command refusal")
	}
}
