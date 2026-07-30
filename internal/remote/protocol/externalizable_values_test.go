package protocol

import (
	"reflect"
	"testing"

	"reasonix/internal/eventwire"
)

func TestExternalizableStringsUsesConcreteOwnerPointersAndSkipsNil(t *testing.T) {
	firstPrompt := "first"
	description := "choice"
	event := SessionEvent{
		Event: eventwire.Event{
			Kind: "ask_request",
			Text: "text",
			Ask: &eventwire.Ask{Questions: []eventwire.AskQuestion{
				{Prompt: firstPrompt, Options: []eventwire.AskOption{{Description: description}}},
				{Prompt: "second", Options: []eventwire.AskOption{}},
			}},
		},
	}
	got, err := ExternalizableStrings(event)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExternalizableString{
		{JSONPointer: "/event/ask/questions/0/options/0/description", Value: "choice"},
		{JSONPointer: "/event/ask/questions/0/prompt", Value: "first"},
		{JSONPointer: "/event/ask/questions/1/prompt", Value: "second"},
		{JSONPointer: "/event/text", Value: "text"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("externalizable values\n got: %#v\nwant: %#v", got, want)
	}
}

func TestExternalizableStringsRejectsNilOwner(t *testing.T) {
	if _, err := ExternalizableStrings(nil); err == nil {
		t.Fatal("nil owner accepted")
	}
}
