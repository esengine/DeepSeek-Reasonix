package agent

import (
	"fmt"
	"reflect"

	"reasonix/internal/provider"
)

// firstWireDiff names the first field that differs between the frozen wire
// bytes and the prefix rebuilt after a resume. providerVisibleFingerprint
// covers only the provider-visible subset, so two message sets can share a
// fingerprint and still serialize to different bytes — a resumed session then
// misses a cache the previous process was hitting at 99%. Reporting the field
// name turns that miss into a pointer instead of a guess.
func firstWireDiff(frozen, rebuilt []provider.Message) string {
	for i := 0; i < len(frozen) && i < len(rebuilt); i++ {
		if f := firstFieldDiff(frozen[i], rebuilt[i]); f != "" {
			return fmt.Sprintf("i%d:%s", i, f)
		}
	}
	if len(frozen) != len(rebuilt) {
		return fmt.Sprintf("len%d/%d", len(frozen), len(rebuilt))
	}
	return ""
}

func firstFieldDiff(a, b provider.Message) string {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	t := va.Type()
	for i := range t.NumField() {
		fa, fb := va.Field(i), vb.Field(i)
		if fa.Type() != fb.Type() {
			return t.Field(i).Tag.Get("json")
		}
		if !reflect.DeepEqual(fa.Interface(), fb.Interface()) {
			return t.Field(i).Tag.Get("json")
		}
	}
	return ""
}
