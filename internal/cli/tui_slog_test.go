package cli

import (
	"log/slog"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestRouteSlogToTUI_WarnBecomesNotice(t *testing.T) {
	ch := make(chan event.Event, 1)
	restore := routeSlogToTUI(ch)
	defer restore()

	slog.Warn("test warning", "key", "value")

	select {
	case e := <-ch:
		if e.Kind != event.Notice {
			t.Fatalf("expected Notice, got %v", e.Kind)
		}
		if e.Level != event.LevelWarn {
			t.Fatalf("expected LevelWarn, got %v", e.Level)
		}
		if !strings.Contains(e.Text, "test warning") {
			t.Fatalf("expected text to contain 'test warning', got %q", e.Text)
		}
		if !strings.Contains(e.Text, "key=value") {
			t.Fatalf("expected text to contain 'key=value', got %q", e.Text)
		}
	default:
		t.Fatal("expected a Notice event, got none")
	}
}

func TestRouteSlogToTUI_InfoDiscarded(t *testing.T) {
	ch := make(chan event.Event, 1)
	restore := routeSlogToTUI(ch)
	defer restore()

	slog.Info("info message")

	select {
	case <-ch:
		t.Fatal("expected no event for Info level")
	default:
	}
}

func TestRouteSlogToTUI_ChannelFullNonBlocking(t *testing.T) {
	ch := make(chan event.Event) // unbuffered
	restore := routeSlogToTUI(ch)
	defer restore()

	// This must not block: no goroutine is reading from ch.
	slog.Warn("should not block")
}

func TestRouteSlogToTUI_Restore(t *testing.T) {
	prev := slog.Default()
	ch := make(chan event.Event, 1)
	restore := routeSlogToTUI(ch)
	restore()

	if slog.Default() != prev {
		t.Fatal("expected default logger to be restored")
	}
}

func TestRouteSlogToTUI_ErrorLevelBecomesNotice(t *testing.T) {
	ch := make(chan event.Event, 1)
	restore := routeSlogToTUI(ch)
	defer restore()

	slog.Error("fatal error", "err", "something broke")

	select {
	case e := <-ch:
		if e.Kind != event.Notice {
			t.Fatalf("expected Notice, got %v", e.Kind)
		}
		if e.Level != event.LevelWarn {
			t.Fatalf("expected LevelWarn, got %v", e.Level)
		}
		if !strings.Contains(e.Text, "fatal error") {
			t.Fatalf("expected text to contain 'fatal error', got %q", e.Text)
		}
	default:
		t.Fatal("expected a Notice event for Error level")
	}
}
