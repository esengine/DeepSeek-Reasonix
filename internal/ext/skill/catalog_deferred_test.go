package skill

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func deferredFixture(t *testing.T, n int) *Store {
	t.Helper()
	home := testenv.TempDir(t)
	big := strings.Repeat("reference material line\n", 10000)
	for i := range n {
		dir := fmt.Sprintf(".reasonix/skills/s%02d", i)
		writeSkill(t, home, dir+"/SKILL.md", fmt.Sprintf("---\ndescription: skill %d\n---\nmain body %d", i, i))
		writeSkill(t, home, dir+"/references/deep.md", big)
		writeScript(t, home, dir+"/scripts/run.sh", "#!/usr/bin/env bash\necho ok")
	}
	return New(Options{HomeDir: home, DisableBuiltins: true})
}

func TestListingLeavesReferencesAndScriptsUnread(t *testing.T) {
	st := deferredFixture(t, 30)
	listed := st.List()
	if len(listed) != 30 {
		t.Fatalf("listed %d skills, want 30", len(listed))
	}
	total := 0
	for _, sk := range listed {
		total += len(sk.Body)
		if strings.Contains(sk.Body, "## Reference:") || strings.Contains(sk.Body, "## Scripts") {
			t.Fatalf("%s: listing carries supplements", sk.Name)
		}
	}
	if total > 2000 {
		t.Fatalf("listing holds %d body bytes for 30 skills, want only the SKILL.md bodies", total)
	}
}

func TestCatalogBytesIgnoreDeferredSupplements(t *testing.T) {
	st := deferredFixture(t, 5)
	listed := st.List()
	completed := make([]Skill, len(listed))
	for i, sk := range listed {
		completed[i] = sk.Complete()
	}
	if IndexBlock(listed) != IndexBlock(completed) {
		t.Fatal("catalog bytes depend on whether supplements were loaded")
	}
}

func TestInvocationPathsLoadSupplements(t *testing.T) {
	st := deferredFixture(t, 3)
	listed := st.List()[0]
	cases := map[string]Skill{"Prepare": st.Prepare(listed)}
	read, ok := st.Read(listed.Name)
	if !ok {
		t.Fatal("Read: not found")
	}
	cases["Read"] = read
	slash, ok := st.ReadSlash(listed.Name)
	if !ok {
		t.Fatal("ReadSlash: not found")
	}
	cases["ReadSlash"] = slash
	forModel, err := st.ForModel(listed.Name)
	if err != nil {
		t.Fatal(err)
	}
	cases["ForModel"] = forModel
	for name, sk := range cases {
		if !strings.Contains(sk.Body, "## Reference: deep") || !strings.Contains(sk.Body, "## Scripts") {
			t.Errorf("%s: references or scripts missing from the invoked body", name)
		}
		if !strings.HasPrefix(sk.Body, "main body") {
			t.Errorf("%s: main body should come first", name)
		}
	}
}
