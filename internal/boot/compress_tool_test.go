package boot

import (
	"reflect"
	"slices"
	"testing"
)

func TestTokenEconomyCompressHonorsExplicitAllowlist(t *testing.T) {
	if got := tokenEconomyBuiltins([]string{"read_file"}); slices.Contains(got, "compress") {
		t.Fatalf("explicit allowlist unexpectedly enabled compress: %v", got)
	}
	if got := tokenEconomyBuiltins([]string{"compress"}); !reflect.DeepEqual(got, []string{"compress"}) {
		t.Fatalf("explicit compress allowlist = %v, want [compress]", got)
	}
}
