package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorRepairSkipsConfigStartupMutation(t *testing.T) {
	if !isDoctorRepairCommand([]string{"doctor", "repair", "--json"}) {
		t.Fatal("doctor repair was not recognized")
	}
	for _, args := range [][]string{{"doctor"}, {"doctor", "capabilities"}, {"run"}} {
		if isDoctorRepairCommand(args) {
			t.Fatalf("unexpected repair match for %v", args)
		}
	}
}

func TestDoctorRepairDryRunDoesNotMigrateConfig(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(root, "reasonix.toml")
	body := []byte("[[plugins]]\nname = \"legacy\"\ncommand = \"legacy\"\ntier = \"lazy\"\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"doctor", "repair", "--root", root, "--json"}, "test"); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("dry run changed config:\n%s", got)
	}
}
