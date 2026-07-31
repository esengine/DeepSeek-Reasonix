package ssh_config

import (
	"errors"
	"testing"
)

type errReader struct {
}

func (b *errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error occurred")
}

func TestIOError(t *testing.T) {
	buf := &errReader{}
	_, err := Decode(buf)
	if err == nil {
		t.Fatal("expected non-nil err, got nil")
	}
	if err.Error() != "read error occurred" {
		t.Errorf("expected read error msg, got %v", err)
	}
}
