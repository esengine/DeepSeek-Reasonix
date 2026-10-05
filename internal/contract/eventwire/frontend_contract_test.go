package eventwire

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The desktop reads events through this package's JSON. A field the TypeScript
// declares but the wire never carries is always undefined at runtime, and
// nothing fails: Profile once declared {name, description} while the kernel
// sent {model, effort}, so every card fell through to a fallback that reported
// a delegate's step count as a number of delegates.
func TestFrontendTypesReadOnlyFieldsTheWireCarries(t *testing.T) {
	tsPath := filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "port", "wire.ts")
	tsSrc, err := os.ReadFile(tsPath)
	if err != nil {
		t.Skipf("frontend wire types unavailable: %v", err)
	}
	goSrc, err := os.ReadFile("wire.go")
	if err != nil {
		t.Fatal(err)
	}

	goFields := map[string]map[string]bool{}
	for _, m := range regexp.MustCompile(`(?s)type (\w+) struct \{(.*?)\n\}`).FindAllStringSubmatch(string(goSrc), -1) {
		fields := map[string]bool{}
		for _, f := range regexp.MustCompile(`json:"([^",]+)`).FindAllStringSubmatch(m[2], -1) {
			if f[1] != "" && f[1] != "-" {
				fields[f[1]] = true
			}
		}
		// A struct with no tagged fields marshals by field name; comparing it
		// against camelCase TypeScript would report every field as missing.
		if len(fields) > 0 {
			goFields[m[1]] = fields
		}
	}

	compared := 0
	for _, m := range regexp.MustCompile(`(?s)export interface (\w+) \{(.*?)\n\}`).FindAllStringSubmatch(string(tsSrc), -1) {
		sent, ok := goFields[m[1]]
		if !ok {
			continue
		}
		compared++
		var missing []string
		for _, f := range regexp.MustCompile(`(?m)^\s*(\w+)\??:`).FindAllStringSubmatch(m[2], -1) {
			if !sent[f[1]] {
				missing = append(missing, f[1])
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			t.Errorf("%s declares %s, which this package never sends", m[1], strings.Join(missing, ", "))
		}
	}
	// A rename on either side would otherwise silently compare nothing.
	if compared < 10 {
		t.Fatalf("only %d interfaces were compared; the two type sets have drifted apart by name", compared)
	}
}
