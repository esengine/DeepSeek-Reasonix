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
	if len(stored) != 8 {
		t.Fatalf("stored fields = %#v, want exactly the frozen non-secret fields", stored)
	}
}

func TestRemoteHostStoreDirectRoundTripAndDuplicateIdentity(t *testing.T) {
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewRemoteDirectHostEntry("Builder@EXAMPLE.com.", 2222, "Builder")
	if err != nil {
		t.Fatal(err)
	}
	if first.Destination != "Builder@example.com" || first.Mode != RemoteHostConnectionDirect || first.Port != 2222 {
		t.Fatalf("canonical direct entry = %#v", first)
	}
	if err := store.Upsert(first); err != nil {
		t.Fatal(err)
	}
	second, err := NewRemoteDirectHostEntry("Builder@example.com", 2222, "Duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(second); err == nil || !strings.Contains(err.Error(), "duplicate SSH Host") {
		t.Fatalf("duplicate direct identity error = %v", err)
	}
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != first {
		t.Fatalf("direct hosts = %#v, want %#v", hosts, first)
	}
}

func TestRemoteHostStoreLoadsV1AsConfigAndWritesV2OnMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	configPath := filepath.Join(t.TempDir(), "legacy ssh config")
	entry, err := NewRemoteHostEntry("legacy-host", "Legacy Host")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf(
		`{"version":1,"hosts":[{"id":%q,"alias":%q,"label":%q,"sshConfigPath":%q,"clientInstanceId":%q,"resumeLeaseId":"lease_legacy","layoutRef":"layout_legacy"}]}`,
		entry.ID, entry.Alias, entry.Label, configPath, entry.ClientInstanceID,
	))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewRemoteHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Mode != RemoteHostConnectionConfig || hosts[0].Alias != "legacy-host" || hosts[0].SSHConfigPath != configPath || hosts[0].ResumeLeaseID != "lease_legacy" {
		t.Fatalf("migrated v1 host = %#v", hosts)
	}
	if err := store.UpdateLayoutRef(entry.ID, "layout_v2"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document remoteHostStoreDocument
	if err := json.Unmarshal(after, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != remoteHostStoreVersion || len(document.Hosts) != 1 || document.Hosts[0].Mode != RemoteHostConnectionConfig || document.Hosts[0].SSHConfigPath != configPath {
		t.Fatalf("persisted migration = %s", after)
	}
}

func TestParseRemoteSSHDirectDestination(t *testing.T) {
	valid := map[string]RemoteSSHDirectTarget{
		"taibai@192.168.1.20":      {Username: "taibai", Host: "192.168.1.20"},
		"build_user@EXAMPLE.COM.":  {Username: "build_user", Host: "example.com"},
		"root@[2001:0db8:0:0::10]": {Username: "root", Host: "2001:db8::10"},
	}
	for raw, want := range valid {
		got, err := ParseRemoteSSHDirectDestination(raw)
		if err != nil || got != want {
			t.Errorf("ParseRemoteSSHDirectDestination(%q) = %#v, %v, want %#v", raw, got, err, want)
		}
	}
	invalid := []string{
		"", "host", "@host", "user@", "user@@host", "-evil@host", "user name@host",
		"user@-oProxyCommand=evil", "user@host name", "user@host;evil", "user@../host",
		"user@2001:db8::10", "user@[192.168.1.1]", "user@[bad::address]", "user@999.999.999.999",
		"user@host:2222", " user@host", "user@host\n",
	}
	for _, raw := range invalid {
		if _, err := ParseRemoteSSHDirectDestination(raw); err == nil {
			t.Errorf("ParseRemoteSSHDirectDestination(%q) unexpectedly succeeded", raw)
		}
	}
	for _, port := range []int{-1, 0, 65536} {
		if err := ValidateRemoteSSHPort(port); err == nil {
			t.Errorf("ValidateRemoteSSHPort(%d) unexpectedly succeeded", port)
		}
	}
	for _, port := range []int{1, 22, 65535} {
		if err := ValidateRemoteSSHPort(port); err != nil {
			t.Errorf("ValidateRemoteSSHPort(%d): %v", port, err)
		}
	}
}

func TestRemoteHostDisplayConnectionFormatsIPv6WithoutDoubleBrackets(t *testing.T) {
	entry, err := NewRemoteDirectHostEntry("builder@[2001:db8::10]", 2222, "Builder")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := remoteHostDisplayConnection(entry), "builder@[2001:db8::10]:2222"; got != want {
		t.Fatalf("remoteHostDisplayConnection() = %q, want %q", got, want)
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
