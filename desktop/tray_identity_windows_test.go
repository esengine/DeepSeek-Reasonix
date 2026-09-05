//go:build windows

package main

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/desktop/internal/instanceidentity"
)

func TestTrayIdentityRequiresVerifiedSignature(t *testing.T) {
	id := instanceidentity.ForHome(t.TempDir())
	for _, tc := range []struct {
		name, dev, exe string
		failure        error
		wantGUID       bool
		calls          int
	}{
		{name: "signed", exe: "desktop.exe", wantGUID: true, calls: 1},
		{name: "unsigned", exe: "desktop.exe", failure: errors.New("unsigned"), calls: 1},
		{name: "unavailable", exe: "desktop.exe", failure: errors.New("access denied"), calls: 1},
		{name: "development", dev: "1", exe: "desktop.exe"}, {name: "missing executable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := signedTrayIdentity(tc.dev, tc.exe, id, func(string) error { calls++; return tc.failure })
			if (got != "") != tc.wantGUID || calls != tc.calls {
				t.Fatalf("guid=%q calls=%d", got, calls)
			}
			if got != "" {
				if _, err := windows.GUIDFromString(got); err != nil {
					t.Fatal(err)
				}
			}
			if got != "" && got != instanceidentity.TrayGUID(id) {
				t.Fatal("wrong identity")
			}
		})
	}
}
