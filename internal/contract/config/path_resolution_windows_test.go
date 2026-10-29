package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/testenv"
)

func TestConfigLoadThroughNonListableDirectory(t *testing.T) {
	for _, user := range []bool{false, true} {
		t.Run(fmt.Sprintf("user=%t", user), func(t *testing.T) {
			home, root := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
			parent, path := root, filepath.Join(root, "reasonix.toml")
			if user {
				parent, path = home, RootsForHome(home).UserConfigPath()
			}
			if err := os.WriteFile(path, []byte("[memory]\nrecall_limit = 937\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			denyConfigDirectoryListing(t, parent)
			if directory, err := os.Open(parent); err == nil {
				_ = directory.Close()
				t.Fatal("fixture still permits a directory read handle")
			} else if !os.IsPermission(err) {
				t.Fatalf("directory-read refusal is not a permission error: %v", err)
			}
			if _, err := os.ReadFile(path); err != nil {
				t.Fatalf("fixture config is not independently readable: %v", err)
			}
			if _, err := resolveConfigReadPath(path); err != nil {
				t.Fatalf("fixture is not accepted by the original logical-path guard: %v", err)
			}
			cfg, err := RootsForHome(home).LoadForRootReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Memory.RecallLimit != 937 {
				t.Fatalf("non-listable directory lost config: recall_limit=%d, want 937", cfg.Memory.RecallLimit)
			}
		})
	}
}

func denyConfigDirectoryListing(t *testing.T, path string) {
	t.Helper()
	original, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	originalACL, _, err := original.DACL()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := original.Control()
	if err != nil {
		t.Fatal(err)
	}
	restoreInfo := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		restoreInfo = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	restricted, err := windows.SecurityDescriptorFromString("D:P(D;;0x1;;;" + sid + ")(A;;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := restricted.DACL()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, restoreInfo, nil, nil, originalACL, nil); err != nil {
			t.Error(err)
		}
	})
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}
