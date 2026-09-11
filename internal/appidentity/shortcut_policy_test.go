package appidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortcutRepairPolicy(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	if err := os.WriteFile(launcher, []byte("launcher"), 0o600); err != nil {
		t.Fatal(err)
	}
	electron := filepath.Join(root, "versions", "v1.38.6", "app", "Reasonix.exe")
	goDesktop := filepath.Join(root, "versions", "v1.20.0", "reasonix-desktop.exe")
	flat := filepath.Join(root, "reasonix-desktop.exe")
	customIcon := filepath.Join(root, "custom.ico")
	for _, tt := range []struct {
		name, target, icon, id string
		want                   shortcutRepair
	}{
		{"new pin", launcher, "", "", shortcutRepair{identity: AppUserModelID}},
		{"legacy pin", launcher, "", "Reasonix", shortcutRepair{identity: AppUserModelID}},
		{"healthy", launcher, launcher, AppUserModelID, shortcutRepair{}},
		{"versioned electron", electron, electron, "Reasonix", shortcutRepair{target: launcher, icon: launcher, identity: AppUserModelID}},
		{"versioned Go", goDesktop, goDesktop, "", shortcutRepair{target: launcher, icon: launcher, identity: AppUserModelID}},
		{"flat electron", filepath.Join(root, "app", "Reasonix.exe"), "", "Reasonix", shortcutRepair{target: launcher, identity: AppUserModelID}},
		{"custom icon", electron, customIcon, "", shortcutRepair{target: launcher, identity: AppUserModelID}},
		{"missing flat", flat, flat, "", shortcutRepair{target: launcher, icon: launcher, identity: AppUserModelID}},
		{"current ID stale target", electron, launcher, AppUserModelID, shortcutRepair{target: launcher}},
		{"stable target stale icon", launcher, electron, AppUserModelID, shortcutRepair{icon: launcher}},
		{"studio ID", electron, electron, "io.reasonix.studio", shortcutRepair{}},
		{"Tauri ID", electron, electron, "dev.reasonix.desktop", shortcutRepair{}},
		{"unknown ID", electron, electron, "Custom.App", shortcutRepair{}},
		{"other install", filepath.Join(t.TempDir(), "Reasonix.exe"), electron, "Reasonix", shortcutRepair{}},
		{"Studio executable", filepath.Join(root, "Reasonix Studio.exe"), electron, "Reasonix", shortcutRepair{}},
		{"unrelated executable", filepath.Join(root, "other.exe"), electron, "", shortcutRepair{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := planShortcutRepair(tt.target, tt.icon, tt.id, root, false); got != tt.want {
				t.Fatalf("repair = %+v, want %+v", got, tt.want)
			}
		})
	}
	if err := os.WriteFile(flat, []byte("flat desktop"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := planShortcutRepair(flat, flat, AppUserModelID, root, false); got != (shortcutRepair{}) {
		t.Fatalf("live flat install must keep its entry: %+v", got)
	}
	if got := planShortcutRepair(flat, flat, AppUserModelID, root, true); got != (shortcutRepair{target: launcher, icon: launcher}) {
		t.Fatalf("activated versioned install must replace stale flat entry: %+v", got)
	}
	if err := os.Remove(launcher); err != nil {
		t.Fatal(err)
	}
	if got := planShortcutRepair(electron, electron, "Reasonix", root, false); got != (shortcutRepair{identity: AppUserModelID}) {
		t.Fatalf("missing launcher must not create a dangling entry: %+v", got)
	}
}

func TestShortcutOwnershipResolvesLinksBeforeCheckingLayout(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "versions", "v1.0.0")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	for _, suffix := range []string{"reasonix-desktop.exe", filepath.Join("app", "Reasonix.exe")} {
		if ownedShortcutTarget(filepath.Join(root, "versions", "v1.0.0", suffix), root) {
			t.Fatalf("external target %s treated as owned", suffix)
		}
	}
	alias := filepath.Join(t.TempDir(), "current")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if !ownedShortcutTarget(filepath.Join(alias, "versions", "v2.0.0", "app", "Reasonix.exe"), root) {
		t.Fatal("alias of the owned root with a pruned version must remain repairable")
	}
	if ownedShortcutTarget(filepath.Join(root, "Reasonix.exe"), filepath.Join(root, "missing-root")) {
		t.Fatal("missing installation root must not establish ownership")
	}
}
