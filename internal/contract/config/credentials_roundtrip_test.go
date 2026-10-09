package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

var roundTripKeys = map[string]string{
	"expansion":                "sk-ab${HOME}x",
	"double quotes":            `"quoted"`,
	"single quotes":            `'quoted'`,
	"escaped dollar":           `sk\$x`,
	"bare dollar":              "sk-a$b",
	"inner quote":              `a"b`,
	"mixed quotes":             `a'b"c$d`,
	"space hash equal":         "a b#c=d",
	"literal backslash":        `\n`,
	"trailing backslash mixed": `a'b"c\`,
	"plain":                    "plain-key_123",
	"trailing backslash":       `sk-abc\`,
	"trailing backslashes":     `sk-abc\\`,
	"command subst":            "sk-$(id)x",
	"dollar in quotes":         "'$'",
	"leading space":            "  sk-abc",
	"trailing space":           "sk-abc  ",
	"unicode":                  "密钥-ключ-🔑",
	"nul":                      "sk-a\x00b",
	"long":                     strings.Repeat("k", 10000),
	"long quoted":              strings.Repeat(`k"$`, 3000),
}

var refusedKeys = map[string]string{
	"trailing backslash with dollar": `sk-$HOME\`,
	"trailing backslash with hash":   `sk #abc\`,
}

func credentialTestHome(t *testing.T) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
}

func TestCredentialValueSurvivesStoreAndResolve(t *testing.T) {
	for _, write := range []string{"set", "set-if-revision"} {
		for name, want := range roundTripKeys {
			t.Run(write+"/"+name, func(t *testing.T) {
				credentialTestHome(t)
				var err error
				if write == "set" {
					_, err = SetCredential("RT_KEY", want)
				} else {
					_, _, err = SetCredentialIfRevision("RT_KEY", want, CredentialStoreRevision())
				}
				if err != nil {
					t.Fatalf("store: %v", err)
				}
				os.Unsetenv("RT_KEY")
				if got := ResolveCredentialForRootGlobalFirst(".", "RT_KEY").Value; got != want {
					raw, _ := os.ReadFile(UserCredentialsPath())
					t.Fatalf("resolved %q, want %q (file %q)", got, want, raw)
				}
			})
		}
	}
}

func TestCredentialValueSurvivesACRLFFile(t *testing.T) {
	credentialTestHome(t)
	if err := os.MkdirAll(filepath.Dir(UserCredentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserCredentialsPath(), []byte("OTHER_KEY=other\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SetCredential("RT_KEY", `sk-${HOME}x`); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("RT_KEY")
	os.Unsetenv("OTHER_KEY")
	if got := ResolveCredentialForRootGlobalFirst(".", "RT_KEY").Value; got != `sk-${HOME}x` {
		t.Fatalf("resolved %q", got)
	}
	if got := ResolveCredentialForRootGlobalFirst(".", "OTHER_KEY").Value; got != "other" {
		t.Fatalf("neighbour resolved %q", got)
	}
}

// A value the file cannot hold is refused before anything is written, because
// one unreadable line would take every credential in the file with it.
func TestUnstorableCredentialLeavesTheFileUntouched(t *testing.T) {
	for name, value := range refusedKeys {
		t.Run(name, func(t *testing.T) {
			credentialTestHome(t)
			if _, err := SetCredential("OTHER_KEY", "other"); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(UserCredentialsPath())
			_, err := SetCredential("RT_KEY", value)
			if !errors.Is(err, ErrCredentialValueUnstorable) {
				t.Fatalf("err = %v, want ErrCredentialValueUnstorable", err)
			}
			after, _ := os.ReadFile(UserCredentialsPath())
			if string(after) != string(before) {
				t.Fatalf("file changed: %q -> %q", before, after)
			}
			os.Unsetenv("OTHER_KEY")
			if got := ResolveCredentialForRootGlobalFirst(".", "OTHER_KEY").Value; got != "other" {
				t.Fatalf("neighbour resolved %q", got)
			}
		})
	}
}
