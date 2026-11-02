package secrets

import (
	"strings"
	"testing"
)

func TestARegisteredValueIsMaskedInEveryFormItTravelsIn(t *testing.T) {
	RegisterValue("SHOP_KEY", `k3y"with/odd chars`)
	defer UnregisterValue(`k3y"with/odd chars`)
	for _, in := range []string{
		`export SHOP_KEY=k3y"with/odd chars`,
		`{"cmd":"curl -H 'X: k3y\"with/odd chars'"}`,
		`https://shop.example/?key=k3y%22with%2Fodd+chars`,
	} {
		got := MaskRegistered(in)
		if strings.Contains(got, "odd chars") || strings.Contains(got, "odd+chars") || !strings.Contains(got, "[secret:SHOP_KEY]") {
			t.Errorf("MaskRegistered(%q) = %q", in, got)
		}
	}
}

func TestAShortValueIsMaskedOnlyWhereItStandsAlone(t *testing.T) {
	RegisterValue("PIN", "4821")
	defer UnregisterValue("4821")
	if got := MaskRegistered("the pin is 4821."); got != "the pin is [secret:PIN]." {
		t.Fatalf("standalone = %q", got)
	}
	if got := MaskRegistered("order 948217 shipped"); got != "order 948217 shipped" {
		t.Fatalf("inside a longer run = %q", got)
	}
}

func TestTheLongerOfTwoValuesIsMaskedFirst(t *testing.T) {
	RegisterValue("SHORT", "abcdefgh")
	RegisterValue("LONG", "abcdefgh-ijklmnop")
	defer UnregisterValue("abcdefgh")
	defer UnregisterValue("abcdefgh-ijklmnop")
	if got := MaskRegistered("token abcdefgh-ijklmnop"); got != "token [secret:LONG]" {
		t.Fatalf("overlapping values = %q", got)
	}
}

func TestAnUnregisteredValueIsLeftAlone(t *testing.T) {
	RegisterValue("GONE", "s3cr3t-value-1")
	UnregisterValue("s3cr3t-value-1")
	if got := MaskRegistered("s3cr3t-value-1"); got != "s3cr3t-value-1" {
		t.Fatalf("after unregistering = %q", got)
	}
	if got := MaskRegistered("nothing here"); got != "nothing here" {
		t.Fatalf("no values = %q", got)
	}
}
