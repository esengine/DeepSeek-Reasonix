package instanceidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestIdentityPreservesExistingFormatAndAliases(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "new", "home")
	sum := sha256.Sum256([]byte(CanonicalHome(home)))
	want := Prefix + "." + hex.EncodeToString(sum[:8])
	if got := ForHome(home); got != want {
		t.Fatalf("identity %s != %s", got, want)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err == nil {
		if got := ForHome(filepath.Join(alias, "new", "home")); got != want {
			t.Fatalf("alias changed identity: %s", got)
		}
	}
	other := ForHome(filepath.Join(root, "other"))
	if other == want || TrayGUID(other) == TrayGUID(want) {
		t.Fatal("independent homes collide")
	}
	id, err := uuid.Parse(TrayGUID(want))
	if err != nil || id.Version() != 5 {
		t.Fatalf("tray ID %v: %v", id, err)
	}
	if TrayGUID(want) != TrayGUID(ForHome(filepath.Join(home, "."))) {
		t.Fatal("equivalent home changed GUID")
	}
}
func TestUpdateEnvironmentFreezesHome(t *testing.T) {
	env := UpdateEnvironment([]string{"PATH=keep", "reasonix_home=old", "REASONIX_UPDATE_INSTANCE_ID=stale"}, "relative-home")
	values := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		values[k] = v
	}
	if len(env) != 3 || values["PATH"] != "keep" || !filepath.IsAbs(values["REASONIX_HOME"]) {
		t.Fatalf("bad environment: %v", env)
	}
	if id := values[UpdateEnvironmentKey]; !Valid(id) || id != ForHome(values["REASONIX_HOME"]) {
		t.Fatalf("bad identity %q", id)
	}
	for _, id := range []string{"", Prefix, Prefix + ".../../x", Prefix + ".0123456789ABCDEf"} {
		if Valid(id) {
			t.Fatalf("accepted %q", id)
		}
	}
}
