package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestOffloadedImagePrefixCountQuanta(t *testing.T) {
	raw := ImageOffloadBudget{Representation: imageRepresentationRaw}
	lengths := []int{4, 4, 4, 4}
	if OffloadedImagePrefixCount(lengths, raw) != 0 {
		t.Fatal("unbounded budget must keep every image")
	}
	if OffloadedImagePrefixCount(lengths, ImageOffloadBudget{MaxBytes: 16}) != 0 {
		t.Fatal("exact byte budget must keep every image")
	}
	if OffloadedImagePrefixCount(lengths, ImageOffloadBudget{MaxImages: 4}) != 0 {
		t.Fatal("exact count budget must keep every image")
	}
	if got := OffloadedImagePrefixCount(append(lengths, 4), ImageOffloadBudget{MaxImages: 4, CountQuantum: 2}); got != 2 {
		t.Fatalf("count quantum = %d, want 2", got)
	}
	if got := OffloadedImagePrefixCount(append(lengths, 1), ImageOffloadBudget{MaxBytes: 16, ByteQuantum: 5}); got != 2 {
		t.Fatalf("byte quantum = %d, want 2", got)
	}
	mib := 1024 * 1024
	ones := make([]int, 129)
	for i := range ones {
		ones[i] = mib
	}
	if got := OffloadedImagePrefixCount(ones, ImageOffloadBudget{MaxBytes: 128 * mib, ByteQuantum: 64 * mib}); got != 65 {
		t.Fatalf("128 MiB / 64 MiB quantum = %d, want 65", got)
	}
}

func TestRequiredImageOffloadSkipsOffloadedAndCountsBase64(t *testing.T) {
	img := func(payload string, offloaded bool) string {
		if offloaded {
			return ImageOffloadedRef
		}
		return "data:image/png;base64," + payload
	}
	// 3 raw bytes encode to 4 base64 chars. Three retained images are 12 bytes.
	payload := "AQID" // 3 decoded bytes
	msgs := []Message{{
		ID: "u1", Role: RoleUser,
		Images: []string{img(payload, false), img(payload, false), img(payload, false)},
	}}
	budget := ImageOffloadBudget{Representation: imageRepresentationBase64, MaxBytes: 8, ByteQuantum: 1, CountQuantum: 1}
	if got := RequiredImageOffload(msgs, budget); got != 1 {
		t.Fatalf("base64 overflow = %d, want 1", got)
	}
	msgs[0].Images[0] = ImageOffloadedRef
	if got := RequiredImageOffload(msgs, budget); got != 0 {
		t.Fatalf("already offloaded prefix = %d, want 0", got)
	}
}

func TestCheckRetainedImagesCountQuantum(t *testing.T) {
	msgs := make([]Message, 0, MaxImagesPerRequest+1)
	for i := range MaxImagesPerRequest + 1 {
		msgs = append(msgs, Message{
			ID: fmt.Sprintf("u%d", i), Role: RoleUser,
			Images: []string{"file-api-" + strings.Repeat("a", 8)},
		})
	}
	err := CheckRetainedImages(msgs)
	off := AsImageOffloadRequired(err)
	if off == nil || off.OffloadImages != ImageOffloadCountQuantum {
		t.Fatalf("601 file images = %v, want offload %d", err, ImageOffloadCountQuantum)
	}
	if CheckRetainedImages(msgs[:MaxImagesPerRequest]) != nil {
		t.Fatal("600 file images must fit")
	}
}

func TestCheckRetainedImagesNineInlineImagesFit(t *testing.T) {
	msgs := make([]Message, 0, 9)
	for i := range 9 {
		msgs = append(msgs, Message{
			ID: fmt.Sprintf("u%d", i), Role: RoleUser,
			Images: []string{"data:image/png;base64,AA=="},
		})
	}
	if err := CheckRetainedImages(msgs); err != nil {
		t.Fatalf("nine tiny inline images must fit: %v", err)
	}
}
