package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadBinarySelection(t *testing.T) {
	local := filepath.Join(t.TempDir(), "reasonix.exe")
	if err := os.WriteFile(local, []byte("local CLI"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, local, localOS, remoteOS, want string
		fetch                                bool
	}{
		{"same platform", local, "linux", "linux", "local CLI", false},
		{"cross platform", local, "windows", "linux", "target CLI", true},
		{"missing sidecar", "", "windows", "linux", "target CLI", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			data, err := uploadBinaryBytes(context.Background(), Options{
				LocalBinary: tc.local, LocalGOOS: tc.localOS, LocalGOARCH: "amd64", ProductVersion: "v1.2.3",
				FetchBinary: func(_ context.Context, version, goos, goarch string) ([]byte, error) {
					called = true
					if version != "v1.2.3" || goos != tc.remoteOS || goarch != "amd64" {
						t.Fatalf("incorrect artifact request: %s %s/%s", version, goos, goarch)
					}
					return []byte("target CLI"), nil
				},
			}, tc.remoteOS, "amd64")
			if err != nil || string(data) != tc.want || called != tc.fetch {
				t.Fatalf("data=%q fetch=%v err=%v", data, called, err)
			}
		})
	}
}

func TestUploadBinaryUnavailable(t *testing.T) {
	sentinel := errors.New("no matching development artifact")
	_, err := uploadBinaryBytes(context.Background(), Options{
		FetchBinary: func(context.Context, string, string, string) ([]byte, error) { return nil, sentinel },
	}, "linux", "arm64")
	if !errors.Is(err, sentinel) {
		t.Fatalf("provider failure lost: %v", err)
	}
	if _, err := uploadBinaryBytes(context.Background(), Options{}, "linux", "amd64"); err == nil {
		t.Fatal("missing binary and provider must fail")
	}
}
