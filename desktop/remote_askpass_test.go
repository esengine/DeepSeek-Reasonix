package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteAskPassClassifiesPrompts(t *testing.T) {
	tests := []struct {
		prompt string
		want   RemoteAskPassPromptKind
	}{
		{"The authenticity of host 'lab' can't be established. Are you sure you want to continue connecting (yes/no/[fingerprint])?", RemoteAskPassHostKeyConfirm},
		{"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", RemoteAskPassHostKeyChanged},
		{"user@lab's password:", RemoteAskPassPassword},
		{"Enter passphrase for key 'C:\\Users\\me\\.ssh\\id_ed25519':", RemoteAskPassKeyPassphrase},
		{"Verification code:", RemoteAskPassVerification},
		{"Keyboard-interactive response:", RemoteAskPassAuthentication},
	}
	for _, test := range tests {
		if got := ClassifyRemoteAskPassPrompt(test.prompt); got != test.want {
			t.Errorf("Classify(%q) = %q, want %q", test.prompt, got, test.want)
		}
	}
}

func TestRemoteAskPassHelperReturnsSecretOnlyToStdoutAndWritesNoDisk(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	const secret = "correct horse battery staple"
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(_ context.Context, prompt RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		if prompt.Kind != RemoteAskPassPassword {
			t.Fatalf("prompt kind = %q, want password", prompt.Kind)
		}
		return RemoteAskPassAnswer{Accepted: true, Value: secret}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, err := broker.SSHEnvironment(filepath.Join(temp, "reasonix-desktop.exe"))
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvironmentMap(environment)
	var stdout bytes.Buffer
	handled, code := RunRemoteAskPassHelper(context.Background(), []string{"user@host's password:"}, env.get, &stdout)
	if !handled || code != 0 || stdout.String() != secret+"\n" {
		t.Fatalf("handled=%v code=%d stdout=%q", handled, code, stdout.String())
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		t.Fatalf("AskPass wrote files to disk: %v", names)
	}
}

func TestRemoteAskPassOneTimeTokenRejectsReplay(t *testing.T) {
	var calls atomic.Int32
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		calls.Add(1)
		return RemoteAskPassAnswer{Accepted: true, Value: "secret"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, _ := broker.SSHEnvironment(filepath.Join(t.TempDir(), "helper"))
	env := parseEnvironmentMap(environment)
	key, endpoint, deadline, err := remoteAskPassHelperConfig(env.get)
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	decoded, _ := base64.RawURLEncoding.DecodeString("CgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgo")
	copy(token[:], decoded)
	if _, err := remoteAskPassExchangeWithToken(context.Background(), endpoint, key, deadline, "Password:", token); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, err := remoteAskPassExchangeWithToken(context.Background(), endpoint, key, deadline, "Password:", token); err == nil || !strings.Contains(err.Error(), "request_replayed") {
		t.Fatalf("replay error = %v, want request_replayed", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

func TestRemoteAskPassExpiredCapabilityFailsBeforePrompt(t *testing.T) {
	var calls atomic.Int32
	broker, err := StartRemoteAskPassBroker(context.Background(), 25*time.Millisecond, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		calls.Add(1)
		return RemoteAskPassAnswer{Accepted: true, Value: "secret"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, err := broker.SSHEnvironment(filepath.Join(t.TempDir(), "helper"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	env := parseEnvironmentMap(environment)
	handled, code := RunRemoteAskPassHelper(context.Background(), []string{"Password:"}, env.get, &bytes.Buffer{})
	if !handled || code == 0 {
		t.Fatalf("handled=%v code=%d, want handled failure", handled, code)
	}
	if calls.Load() != 0 {
		t.Fatalf("expired capability reached handler %d times", calls.Load())
	}
}

func TestRemoteAskPassChangedHostKeyFailsClosedAndSanitizesUI(t *testing.T) {
	var calls atomic.Int32
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		calls.Add(1)
		return RemoteAskPassAnswer{Accepted: true, Value: "yes"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, _ := broker.SSHEnvironment(filepath.Join(t.TempDir(), "helper"))
	env := parseEnvironmentMap(environment)
	var stdout bytes.Buffer
	prompt := "\x1b[31mWARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!\x1b[0m\nOffending ED25519 key"
	_, code := RunRemoteAskPassHelper(context.Background(), []string{prompt}, env.get, &stdout)
	if code == 0 {
		t.Fatal("changed Host key was accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("changed Host key was delegated to an approval callback")
	}
	if stdout.Len() != 0 {
		t.Fatalf("changed Host key emitted stdout %q", stdout.String())
	}
}

func TestRemoteAskPassChangedHostKeyAfterDisplayTruncationStillFailsClosed(t *testing.T) {
	var calls atomic.Int32
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		calls.Add(1)
		return RemoteAskPassAnswer{Accepted: true, Value: "secret"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, _ := broker.SSHEnvironment(filepath.Join(t.TempDir(), "helper"))
	env := parseEnvironmentMap(environment)
	prompt := strings.Repeat("informational text ", 300) + " WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!"
	_, code := RunRemoteAskPassHelper(context.Background(), []string{prompt}, env.get, &bytes.Buffer{})
	if code == 0 || calls.Load() != 0 {
		t.Fatalf("late changed-key warning was not fail-closed: code=%d calls=%d", code, calls.Load())
	}
}

func TestRemoteAskPassHostKeyPromptPreservesFingerprintWithoutControls(t *testing.T) {
	var received RemoteAskPassPrompt
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(_ context.Context, prompt RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		received = prompt
		return RemoteAskPassAnswer{Accepted: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	environment, _ := broker.SSHEnvironment(filepath.Join(t.TempDir(), "helper"))
	env := parseEnvironmentMap(environment)
	raw := "\x1b]0;owned\x07The authenticity of host 'lab' can't be established.\nED25519 key fingerprint is SHA256:AbCdEf0123.\nAre you sure you want to continue connecting?"
	var stdout bytes.Buffer
	_, code := RunRemoteAskPassHelper(context.Background(), []string{raw}, env.get, &stdout)
	if code != 0 || stdout.String() != "yes\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	if received.Kind != RemoteAskPassHostKeyConfirm || !strings.Contains(received.Message, "SHA256:AbCdEf0123") {
		t.Fatalf("sanitized prompt = %#v", received)
	}
	if strings.ContainsRune(received.Message, '\x1b') || strings.ContainsAny(received.Message, "\r\n\t") {
		t.Fatalf("sanitized prompt retained controls: %q", received.Message)
	}
}

func TestRemoteAskPassRejectsNonLoopbackAndTamperedConfig(t *testing.T) {
	base := remoteEnvironmentMap{
		remoteAskPassModeEnv:     "1",
		remoteAskPassEndpointEnv: "192.0.2.10:1234",
		remoteAskPassKeyEnv:      base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		remoteAskPassDeadlineEnv: "9999999999999",
	}
	if _, _, _, err := remoteAskPassHelperConfig(base.get); err == nil {
		t.Fatal("non-loopback endpoint accepted")
	}
	base[remoteAskPassEndpointEnv] = "127.0.0.1:1234"
	base[remoteAskPassKeyEnv] = "short"
	if _, _, _, err := remoteAskPassHelperConfig(base.get); err == nil {
		t.Fatal("short capability key accepted")
	}
}

func TestRemoteAskPassHelperIgnoresNormalDesktopMode(t *testing.T) {
	handled, code := RunRemoteAskPassHelper(context.Background(), nil, func(string) string { return "" }, &bytes.Buffer{})
	if handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
}

type remoteEnvironmentMap map[string]string

func (m remoteEnvironmentMap) get(key string) string { return m[key] }

func parseEnvironmentMap(values []string) remoteEnvironmentMap {
	result := make(remoteEnvironmentMap, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if ok {
			result[key] = item
		}
	}
	return result
}
