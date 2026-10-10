package bootstrap

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/platform/remote"
)

func TestEnsureServeKeepsAnAdoptedServeWhenItsTokenIsUnavailable(t *testing.T) {
	skipOnWindows(t)
	for _, token := range []string{"missing", "empty"} {
		t.Run(token, func(t *testing.T) {
			pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
			alive := aliveServe(t, "4242", "reasonix v2.31.0\n")
			conn := newFakeConn(t, pub.root, func(cmd string) (remote.ExecResult, error) {
				if strings.Contains(cmd, "command -v reasonix") {
					return ok("bin /usr/bin/reasonix\nver reasonix v2.31.0\n" + allFlagsYes())
				}
				return alive(cmd)
			})
			opts := Options{Workspace: "~", MinVersion: MinPaneVersion}
			first, err := EnsureServe(context.Background(), conn, opts)
			if err != nil || !first.Reused {
				t.Fatalf("initial adoption: reused=%v err=%v", first.Reused, err)
			}
			before, err := os.ReadFile(pub.paths.StateJSON)
			if err != nil {
				t.Fatal(err)
			}
			if token == "missing" {
				err = os.Remove(pub.paths.TokenFile)
			} else {
				err = os.WriteFile(pub.paths.TokenFile, []byte(" \n"), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}

			_, err = EnsureServe(context.Background(), conn, opts)
			if !errors.Is(err, ErrServeNotAttachable) {
				t.Fatalf("reconnect error = %v, want ErrServeNotAttachable", err)
			}
			stopped := conn.ranContaining("kill -TERM")
			launched := conn.ranContaining("nohup")
			located := conn.ranContaining("command -v reasonix")
			if stopped || launched || located {
				t.Fatalf("an unavailable token entered the replacement path: stop=%v launch=%v locate=%v", stopped, launched, located)
			}
			pub.intact(t)
			after, err := os.ReadFile(pub.paths.StateJSON)
			if err != nil || string(after) != string(before) {
				t.Fatalf("the live serve's record changed: err=%v", err)
			}
			if err := os.WriteFile(pub.paths.TokenFile, []byte("restored-token\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := EnsureServe(context.Background(), conn, opts)
			if err != nil || !res.Reused || res.State.PID != first.State.PID || res.Token != "restored-token" {
				t.Fatalf("reconnect after restoring token: res=%+v err=%v", res, err)
			}
		})
	}
}

func TestEnsureServeReusesARecordedServeWithoutPublishedEndpointFiles(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
	conn := newFakeConn(t, pub.root, aliveServe(t, "4242", "reasonix v2.31.0\n"))
	opts := Options{Workspace: "~", MinVersion: MinPaneVersion}
	if _, err := EnsureServe(context.Background(), conn, opts); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pub.paths.PortFile, pub.paths.PidFile} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	res, err := EnsureServe(context.Background(), conn, opts)
	if err != nil || !res.Reused || res.State.PID != 4242 || res.State.Addr != "127.0.0.1:47001" {
		t.Fatalf("recorded endpoint not reused: res=%+v err=%v", res, err)
	}
	if conn.ranContaining("kill -TERM") || conn.ranContaining("nohup") || conn.ranContaining("command -v reasonix") {
		t.Fatal("missing publication files caused a replacement of the live serve")
	}
}
