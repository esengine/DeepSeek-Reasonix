package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRemoteHostStoreRoundTripPrivateAndSecretFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote", "hosts.json")
	store, err := NewRemoteHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := NewRemoteHostEntry("lab-linux", "Lab Linux")
	if err != nil {
		t.Fatal(err)
	}
	entry.ResumeLeaseID = "lease_opaque"
	entry.LayoutRef = "layout_lab"
	entry.SSHConfigPath = filepath.Join(filepath.Dir(path), "ssh config")
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != entry {
		t.Fatalf("hosts = %#v, want %#v", hosts, entry)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "passphrase", "privateKey", "askPass", "secret"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(`"`+forbidden+`"`)) {
			t.Fatalf("store contains forbidden secret field %q: %s", forbidden, raw)
		}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	entries := object["hosts"].([]any)
	stored := entries[0].(map[string]any)
	if len(stored) != 7 {
		t.Fatalf("stored fields = %#v, want exactly the frozen non-secret fields", stored)
	}
}

func TestRemoteHostStoreCorruptionFailsClosedAndIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	raw := []byte(`{"version":1,"hosts":[{"alias":"host","label":"Host","clientInstanceId":"client","password":"must-not-load"}]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewRemoteHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrRemoteHostStoreCorrupt) {
		t.Fatalf("Load error = %v, want ErrRemoteHostStoreCorrupt", err)
	}
	entry, err := NewRemoteHostEntry("replacement", "Replacement")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(entry); !errors.Is(err, ErrRemoteHostStoreCorrupt) {
		t.Fatalf("Upsert error = %v, want ErrRemoteHostStoreCorrupt", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(raw) {
		t.Fatalf("corrupt store was overwritten:\n%s", after)
	}
}

func TestRemoteHostStoreConcurrentInstancesDoNotLoseUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	const count = 48
	var wg sync.WaitGroup
	errorsCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store, err := NewRemoteHostStore(path)
			if err != nil {
				errorsCh <- err
				return
			}
			entry, err := NewRemoteHostEntry(fmt.Sprintf("host-%02d", i), fmt.Sprintf("Host %02d", i))
			if err == nil {
				err = store.Upsert(entry)
			}
			if err != nil {
				errorsCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	store, _ := NewRemoteHostStore(path)
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != count {
		t.Fatalf("host count = %d, want %d", len(hosts), count)
	}
	for i := 1; i < len(hosts); i++ {
		if hosts[i-1].ID >= hosts[i].ID {
			t.Fatalf("hosts not deterministically sorted: %q then %q", hosts[i-1].ID, hosts[i].ID)
		}
	}
}

func TestRemoteHostStoreLeaseAndLayoutUpdates(t *testing.T) {
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := NewRemoteHostEntry("buildbox", "Build Box")
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateResumeLease(entry.ID, "lease_new"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateLayoutRef(entry.ID, "layout_new"); err != nil {
		t.Fatal(err)
	}
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if hosts[0].ClientInstanceID != entry.ClientInstanceID || hosts[0].ResumeLeaseID != "lease_new" || hosts[0].LayoutRef != "layout_new" {
		t.Fatalf("updated host = %#v", hosts[0])
	}
	if err := store.UpdateResumeLease(entry.ID, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteHostAliasRejectsArgumentAndShellInjection(t *testing.T) {
	for _, alias := range []string{
		"", "-oProxyCommand=calc", "host name", "user@host", "host;touch-pwned",
		"host\nProxyCommand evil", "../host", strings.Repeat("a", 256),
	} {
		if err := ValidateRemoteHostAlias(alias); err == nil {
			t.Errorf("ValidateRemoteHostAlias(%q) unexpectedly succeeded", alias)
		}
	}
	for _, alias := range []string{"host", "lab-linux", "prod.example_2"} {
		if err := ValidateRemoteHostAlias(alias); err != nil {
			t.Errorf("ValidateRemoteHostAlias(%q): %v", alias, err)
		}
	}
}

func TestRemoteHostStoreRejectsSymlinkFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows builders")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"hosts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hosts.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store, _ := NewRemoteHostStore(link)
	if _, err := store.Load(); !errors.Is(err, ErrRemoteHostStoreUnsafe) {
		t.Fatalf("Load symlink error = %v, want ErrRemoteHostStoreUnsafe", err)
	}
}

func TestNewRemoteHostEntryUsesIndependent256BitIdentity(t *testing.T) {
	first, err := NewRemoteHostEntry("one", "One")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRemoteHostEntry("two", "Two")
	if err != nil {
		t.Fatal(err)
	}
	if first.ClientInstanceID == second.ClientInstanceID {
		t.Fatal("independent Host entries reused clientInstanceId")
	}
	for _, id := range []string{first.ClientInstanceID, second.ClientInstanceID} {
		if !strings.HasPrefix(id, "desktop_") || len(strings.TrimPrefix(id, "desktop_")) != 64 {
			t.Fatalf("clientInstanceId %q is not a 256-bit opaque identity", id)
		}
	}
	if first.ID == second.ID || !strings.HasPrefix(first.ID, "host_") || len(strings.TrimPrefix(first.ID, "host_")) != 64 {
		t.Fatalf("entry identities are not independent 256-bit values: %q %q", first.ID, second.ID)
	}
}

func TestRemoteHostStoreStableIDSurvivesAliasRename(t *testing.T) {
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := NewRemoteHostEntry("old-alias", "Host")
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	entry.Alias = "new-alias"
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Get(entry.ID)
	if err != nil || !ok {
		t.Fatalf("Get = %#v, %v, %v", loaded, ok, err)
	}
	if loaded.ID != entry.ID || loaded.Alias != "new-alias" || loaded.ClientInstanceID != entry.ClientInstanceID {
		t.Fatalf("renamed entry = %#v", loaded)
	}
	hosts, _ := store.Load()
	if len(hosts) != 1 {
		t.Fatalf("alias rename created %d records", len(hosts))
	}
}

func TestRemoteHostStoreValidatesOptionalSSHConfigPath(t *testing.T) {
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := NewRemoteHostEntry("host", "Host")
	dir := t.TempDir()
	unclean := dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "unclean"
	for _, invalid := range []string{"relative/config", unclean, "/tmp/config\nProxyCommand evil"} {
		entry.SSHConfigPath = invalid
		if err := store.Upsert(entry); err == nil {
			t.Errorf("sshConfigPath %q unexpectedly accepted", invalid)
		}
	}
	entry.SSHConfigPath = filepath.Join(t.TempDir(), "ssh config")
	if err := store.Upsert(entry); err != nil {
		t.Fatalf("absolute clean sshConfigPath: %v", err)
	}
}
