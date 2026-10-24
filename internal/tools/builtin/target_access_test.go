package builtin

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestAReadRefusalNamesTheTargetAndWhyWithoutNamingTheProtection(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "notes.txt")
	err = checkReadTargetAccess([]string{root}, nil, outside)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeReadOutsideScope {
		t.Fatalf("err = %v, want the outside-scope refusal", err)
	}
	if !strings.Contains(refusal.Message, outside) || !strings.Contains(refusal.Message, root) {
		t.Fatalf("message %q names neither the target nor the readable folder", refusal.Message)
	}

	secret := filepath.Join(root, "keys")
	target := filepath.Join(secret, "id")
	err = checkReadTargetAccess([]string{root}, []string{secret}, target)
	if !errors.As(err, &refusal) || refusal.Code != CodeReadForbidden {
		t.Fatalf("err = %v, want the forbidden refusal", err)
	}
	if !strings.Contains(refusal.Message, target) {
		t.Fatalf("message %q does not name the target", refusal.Message)
	}
	if strings.Contains(refusal.Message, secret+" ") || strings.Contains(refusal.Message, "(") {
		t.Fatalf("message %q names the protection that matched", refusal.Message)
	}
}
