package tool

import (
	"encoding/json"
	"testing"
)

func TestARefusalRendersTheSettingThatWouldLiftIt(t *testing.T) {
	r := Refusal{Code: "peer.disabled", Message: "Messages between sessions are off.", Remedy: &Remedy{Op: RemedyPropose, Setting: "peers.enabled", Value: json.RawMessage(`true`)}}
	want := "Messages between sessions are off. (refusal: peer.disabled) (fix: tool:setting op=propose id=peers.enabled value=true)"
	if got := r.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestARemedyThatOnlyOpensCarriesNoValue(t *testing.T) {
	r := Refusal{Code: "x.y", Remedy: &Remedy{Op: RemedyOpen, Setting: "permissions.mode", Value: json.RawMessage(`"yolo"`)}}
	if got, want := r.String(), "(refusal: x.y) (fix: tool:setting op=open id=permissions.mode)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestARefusalWithoutARemedyReadsAsBefore(t *testing.T) {
	if got := (Refusal{Code: "a.b", Message: "no"}).String(); got != "no (refusal: a.b)" {
		t.Fatalf("got %q", got)
	}
}
