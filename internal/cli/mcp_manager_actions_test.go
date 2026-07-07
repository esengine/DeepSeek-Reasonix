package cli

import (
	"reflect"
	"testing"
)

func TestSplitEditorCommandUsesStaticShellWords(t *testing.T) {
	got, err := splitEditorCommand(`code --goto "dir/file name.go:12"`)
	if err != nil {
		t.Fatalf("splitEditorCommand: %v", err)
	}
	want := []string{"code", "--goto", "dir/file name.go:12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestSplitEditorCommandRejectsShellControl(t *testing.T) {
	if _, err := splitEditorCommand(`vim file; rm -rf tmp`); err == nil {
		t.Fatal("splitEditorCommand accepted shell control syntax")
	}
}
