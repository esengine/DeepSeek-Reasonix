package boot

import (
	"strings"

	"reasonix/internal/contract/event"
)

type skillDiagnosticWriter struct{ sink event.Sink }

func (w skillDiagnosticWriter) Write(p []byte) (int, error) {
	report(w.sink, event.Event{Level: event.LevelWarn, Text: strings.TrimRight(string(p), "\r\n")})
	return len(p), nil
}
