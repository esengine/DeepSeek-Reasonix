package team

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsClosed(t *testing.T) {
	if !errors.Is(checkPlatform("windows"), ErrUnsupportedPlatform) {
		t.Fatal("Windows team execution was allowed")
	}
	for _, os := range []string{"darwin", "linux"} {
		if err := checkPlatform(os); err != nil {
			t.Fatal(err)
		}
	}
}
