package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessiontemp"
)

func TestALauncherRequestRoundTripsAndOnlyThatRequestIsOne(t *testing.T) {
	sid := newSyntheticSID()
	argv := winLaunchArgv(`C:\Program Files\Reasonix\reasonix.exe`, winLaunch{SID: sid, Env: map[string]string{"TEMP": `D:\t`}}, []string{"bash", "-c", "echo hi"})
	l, rest, ok := parseWinLaunch(argv)
	if !ok || l.SID != sid || l.Env["TEMP"] != `D:\t` || !slices.Equal(rest, []string{"bash", "-c", "echo hi"}) {
		t.Fatalf("parse = %+v %v %v", l, rest, ok)
	}
	for name, args := range map[string][]string{
		"ordinary start":    {"reasonix", "chat"},
		"no command":        {"reasonix", winLaunchFlag, "e30", "--"},
		"missing separator": {"reasonix", winLaunchFlag, "e30", "x", "bash"},
		"not base64":        {"reasonix", winLaunchFlag, "***", "--", "bash"},
		"flag not first":    {"reasonix", "x", winLaunchFlag, "e30", "--", "bash"},
	} {
		if _, _, ok := parseWinLaunch(args); ok {
			t.Errorf("%s parsed as a launcher request", name)
		}
	}
}

// A request naming a SID this package could not have made is not honoured: it
// would restrict a token to a real account, and that is no restriction.
func TestALauncherRequestNamingARealAccountIsRefused(t *testing.T) {
	for _, sid := range []string{"S-1-1-0", "S-1-5-18", "S-1-5-21-1-2-3-1001", ""} {
		argv := winLaunchArgv("reasonix", winLaunch{SID: sid}, []string{"cmd"})
		if _, _, ok := parseWinLaunch(argv); ok {
			t.Errorf("a request for %q was accepted", sid)
		}
	}
}

func leaseLaunch(t *testing.T) (winLaunch, string) {
	t.Helper()
	lease, err := os.MkdirTemp("", sessionTempPrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(lease) })
	return winLaunch{SID: newSyntheticSID(), Grant: []string{lease}}, lease
}

func TestTheLauncherGrantsOnlySessionTempFolders(t *testing.T) {
	l, _ := leaseLaunch(t)
	if err := checkWinLaunch(l); err != nil {
		t.Fatalf("the host's own request was refused: %v", err)
	}
	for name, mutate := range map[string]func(*winLaunch){
		"outside the temp": func(x *winLaunch) { x.Grant = []string{testenv.TempDir(t)} },
		"wrong name": func(x *winLaunch) {
			d, _ := os.MkdirTemp("", "other-")
			t.Cleanup(func() { _ = os.RemoveAll(d) })
			x.Grant = []string{d}
		},
		"nested under a lease": func(x *winLaunch) { x.Grant = []string{filepath.Join(x.Grant[0], "sub")} },
		"the temp root":        func(x *winLaunch) { x.Grant = []string{os.TempDir()} },
	} {
		req := l
		req.Grant = slices.Clone(l.Grant)
		mutate(&req)
		if err := checkWinLaunch(req); !errors.Is(err, errLaunchRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A domain user's SID has the shape of a synthetic one; only the account lookup
// tells them apart.
func TestASIDThatNamesAnAccountIsNotSynthetic(t *testing.T) {
	const domainUser = "S-1-5-21-1111-2222-3333-12345"
	if !syntheticSID(domainUser) {
		t.Fatal("setup: the shape alone should pass")
	}
	old := sidOwned
	sidOwned = func(s string) bool { return s == domainUser }
	t.Cleanup(func() { sidOwned = old })
	if syntheticSID(domainUser) {
		t.Fatal("a SID naming an account was accepted")
	}
	state := testenv.TempDir(t)
	rec := `{"version":1,"sid":"` + domainUser + `"}`
	if err := os.WriteFile(filepath.Join(state, grantBookFile), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenGrantBook(state, newFakeACL()); !errors.Is(err, ErrGrantBookUnreadable) {
		t.Errorf("a record naming an account opened: %v", err)
	}
	argv := winLaunchArgv("reasonix", winLaunch{SID: domainUser}, []string{"cmd"})
	if _, _, ok := parseWinLaunch(argv); ok {
		t.Error("a launcher request naming an account parsed")
	}
}

func TestTheLauncherAndTheSessionTempManagerSpellThePrefixAlike(t *testing.T) {
	if sessionTempPrefix != sessiontemp.Prefix() {
		t.Fatalf("launcher prefix %q, manager prefix %q", sessionTempPrefix, sessiontemp.Prefix())
	}
}
