package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPresentedFilesStayInProjectionButNeverEnterProviderBytes(t *testing.T) {
	base := []Message{{Role: RoleTool, Name: "present", ToolCallID: "call-present", Content: "Presented game.html"}}
	stored := append([]Message(nil), base...)
	stored[0].PresentedFiles = NewPresentedFilesMetadata([]PresentedFile{{Path: "game.html", Description: "Game"}})
	persisted, err := json.Marshal(stored[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), `"presented_files":{"version":1,"files":[`) {
		t.Fatalf("presented files are not in the versioned metadata envelope: %s", persisted)
	}

	projection := ProjectionMessages(stored)
	if !reflect.DeepEqual(projection[0].PresentedFiles, stored[0].PresentedFiles) {
		t.Fatalf("ProjectionMessages dropped presented files: %+v", projection[0])
	}
	want, err := json.Marshal(ModelMessages(base))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(ModelMessages(stored))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("presented file metadata changed provider bytes\n got: %s\nwant: %s", got, want)
	}
	if stored[0].PresentedFiles == nil || len(stored[0].PresentedFiles.Files) != 1 {
		t.Fatal("projection mutated the stored transcript")
	}
}

func TestUnknownPresentedFilesMetadataVersionFailsClosed(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"tool","presented_files":{"version":2,"files":[{"path":"future.bin"}]}}`), &message); err != nil {
		t.Fatal(err)
	}
	if files := PresentedFileList(message.PresentedFiles); len(files) != 0 {
		t.Fatalf("future metadata version was treated as trusted: %#v", files)
	}
}
