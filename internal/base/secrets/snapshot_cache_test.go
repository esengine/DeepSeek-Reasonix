package secrets

import "testing"

func TestMaskSnapshotIsReusedUntilSourceValuesChange(t *testing.T) {
	values := []Labeled{{Label: "CACHE_PROBE", Value: "cache-probe-value-0001"}}
	WatchValues(t.Name(), func() []Labeled { return values })
	first := Masker()
	if second := Masker(); first != second {
		t.Fatal("unchanged sources rebuilt the mask")
	}
	values = []Labeled{{Label: "CACHE_PROBE", Value: "cache-probe-value-0002"}}
	changed := Masker()
	if changed == first {
		t.Fatal("changed source reused the old mask")
	}
	for _, value := range []string{"cache-probe-value-0001", "cache-probe-value-0002"} {
		if changed.String(value) == value {
			t.Fatal("new or retired value escaped masking")
		}
	}
	values = nil
	if Masker() != changed {
		t.Fatal("retiring a value rebuilt an unchanged retained mask")
	}
}
