package provider

import (
	"strconv"
	"strings"
	"testing"
)

func TestSelectAndApplyImageOffloadKeepsIndexesStable(t *testing.T) {
	msgs := []Message{
		{ID: "u1", Role: RoleUser, Content: "one", Images: []string{"data:image/png;base64,AA==", "data:image/png;base64,BB=="}},
		{ID: "a1", Role: RoleAssistant, Content: "ok", Images: []string{"data:image/png;base64,CC=="}},
		{ID: "u2", Role: RoleUser, Content: "two", Images: []string{"data:image/png;base64,DD=="}},
	}
	targets := OffloadOldestImages(msgs, 2)
	if len(targets) != 1 || targets[0].MessageID != "u1" || len(targets[0].ImageIndexes) != 2 {
		t.Fatalf("targets = %+v", targets)
	}
	got := ApplyImageOffload(msgs, targets)
	if got[0].Images[0] != ImageOffloadedRef || got[0].Images[1] != ImageOffloadedRef {
		t.Fatalf("u1 images = %v", got[0].Images)
	}
	if got[1].Images[0] != "data:image/png;base64,CC==" {
		t.Fatal("assistant images must not be offloaded")
	}
	if got[2].Images[0] != "data:image/png;base64,DD==" {
		t.Fatalf("kept image = %v", got[2].Images)
	}
	if !strings.Contains(got[0].Content, OffloadedImageNotice()) {
		t.Fatalf("missing notice: %q", got[0].Content)
	}
	again := ApplyImageOffload(got, targets)
	if again[0].Content != got[0].Content {
		t.Fatal("re-applying offload must be idempotent")
	}
}

func TestMergeImageOffloadIsIdempotent(t *testing.T) {
	first := []ImageOffloadTarget{{MessageID: "u1", ImageIndexes: []int{0, 2}}}
	second := []ImageOffloadTarget{{MessageID: "u1", ImageIndexes: []int{2, 1}}, {MessageID: "u2", ImageIndexes: []int{0}}}
	got := MergeImageOffload(first, second)
	if len(got) != 2 || got[0].MessageID != "u1" || fmtIndexes(got[0].ImageIndexes) != "0,1,2" {
		t.Fatalf("merged = %+v", got)
	}
	again := MergeImageOffload(got, second)
	if fmtIndexes(again[0].ImageIndexes) != "0,1,2" || len(again) != 2 {
		t.Fatalf("re-merge = %+v", again)
	}
}

func fmtIndexes(idx []int) string {
	out := make([]string, len(idx))
	for i, n := range idx {
		out[i] = strconv.Itoa(n)
	}
	return strings.Join(out, ",")
}
