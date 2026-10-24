package tool

import (
	"encoding/hex"
	"errors"
	"strings"
)

// BashVerificationPayload binds the diagnostic trailer to the exact command.
// Callers may supply a nonce marker, never a replacement executable payload.
func BashVerificationPayload(command, marker string) (string, error) {
	if marker == "" {
		return command, nil
	}
	nonce, ok := strings.CutPrefix(marker, "__rx_pipestatus_")
	if _, err := hex.DecodeString(nonce); !ok || len(nonce) != 16 || err != nil {
		return "", errors.New("invalid verification pipeline marker")
	}
	return command + "\n__rx_ps=(\"${PIPESTATUS[@]}\"); printf '%s %s\\n' '" + marker +
		"' \"${__rx_ps[*]}\" >&2; exit \"${__rx_ps[${#__rx_ps[@]}-1]:-0}\"\n", nil
}
