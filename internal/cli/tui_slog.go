package cli

import (
	"log/slog"
	"strings"
	"sync"

	"reasonix/internal/event"
)

// tuiNoticeWriter is an io.Writer that turns each Write into a TUI Notice
// event on the given channel. Writes are non-blocking: when the channel is
// full the log line is discarded rather than blocking the caller.
type tuiNoticeWriter struct {
	ch chan<- event.Event
}

func (w tuiNoticeWriter) Write(p []byte) (int, error) {
	line := strings.TrimSuffix(string(p), "\n")
	if line == "" {
		return len(p), nil
	}
	ev := event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: line}
	select {
	case w.ch <- ev:
	default:
	}
	return len(p), nil
}

// dropTimeAndLevel removes time and level keys so the TextHandler output is
// just msg and attributes.
func dropTimeAndLevel(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
		return slog.Attr{}
	}
	return a
}

// routeSlogToTUI replaces the default slog.Logger with one that writes Warn
// and above as TUI Notice events into ch. Info and Debug are discarded.
// Returns a function that restores the original logger.
func routeSlogToTUI(ch chan<- event.Event) func() {
	prev := slog.Default()
	handler := slog.NewTextHandler(tuiNoticeWriter{ch: ch}, &slog.HandlerOptions{
		Level:       slog.LevelWarn,
		ReplaceAttr: dropTimeAndLevel,
	})
	slog.SetDefault(slog.New(handler))
	var once sync.Once
	return func() { once.Do(func() { slog.SetDefault(prev) }) }
}
