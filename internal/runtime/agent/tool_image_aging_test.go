package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/visionimage"
)

func pngDataURL(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func toolImageMessages(counts ...int) []provider.Message {
	var msgs []provider.Message
	n := 0
	for i, c := range counts {
		msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: string(rune('a' + i)), Name: "shot"}}})
		m := provider.Message{Role: provider.RoleTool, Name: "shot", ToolCallID: string(rune('a' + i)), Content: "shot"}
		for range c {
			m.Images = append(m.Images, "img"+string(rune('0'+n)))
			n++
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func toolImagesOf(msgs []provider.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == provider.RoleTool {
			out = append(out, m.Images...)
		}
	}
	return out
}

func TestAgedToolImagesKeepTheNewestAndDetachWholeBlocks(t *testing.T) {
	cases := []struct {
		counts []int
		want   int
	}{
		{[]int{1, 1, 1}, 3},
		{[]int{1, 1, 1, 1, 1}, 5},
		{[]int{1, 1, 1, 1, 1, 1}, 3},
		{[]int{1, 1, 1, 1, 1, 1, 1, 1}, 5},
		{[]int{1, 1, 1, 1, 1, 1, 1, 1, 1}, 3},
		{[]int{5}, 5},
		{[]int{5, 1}, 3},
	}
	for _, tc := range cases {
		msgs := toolImageMessages(tc.counts...)
		all := toolImagesOf(msgs)
		got := toolImagesOf(withAgedToolImages(msgs))
		if len(got) != tc.want {
			t.Errorf("counts %v kept %d images, want %d", tc.counts, len(got), tc.want)
			continue
		}
		if !reflect.DeepEqual(got, all[len(all)-tc.want:]) {
			t.Errorf("counts %v kept %v, want the newest %v", tc.counts, got, all[len(all)-tc.want:])
		}
		if !reflect.DeepEqual(toolImagesOf(msgs), all) {
			t.Errorf("counts %v: aging wrote through to the history it was given", tc.counts)
		}
	}
}

func TestAgedToolImagesSayWhatLeftAndSettleOnASecondPass(t *testing.T) {
	msgs := toolImageMessages(2, 1, 1, 1, 1)
	once := withAgedToolImages(msgs)
	if !strings.Contains(once[1].Content, "2 image(s) this result returned are no longer attached") {
		t.Fatalf("a message that lost both its images must say so, got %q", once[1].Content)
	}
	if !strings.Contains(once[3].Content, "1 image(s)") || len(once[3].Images) != 0 {
		t.Fatalf("the second message lost its only image: %+v", once[3])
	}
	if once[5].Content != "shot" {
		t.Fatalf("a message that kept its image must be untouched, got %q", once[5].Content)
	}
	if twice := withAgedToolImages(once); !reflect.DeepEqual(twice, once) {
		t.Fatal("aging an aged request changed it again")
	}
}

func TestAgedToolImagesNeverDetachWhatTheUserAttached(t *testing.T) {
	msgs := append([]provider.Message{{Role: provider.RoleUser, Content: "look", Images: []string{"u1", "u2", "u3", "u4"}}},
		toolImageMessages(1, 1, 1, 1, 1, 1)...)
	aged := withAgedToolImages(msgs)
	if len(aged[0].Images) != 4 {
		t.Fatalf("user images were detached: %v", aged[0].Images)
	}
	if got := len(toolImagesOf(aged)); got != 3 {
		t.Fatalf("user images must not count toward the tool budget: kept %d tool images", got)
	}
}

// Detaching edits an earlier message, which is what costs the prefix cache.
// Appending a round must leave every earlier message byte-identical except on
// the rounds where a whole block leaves.
func TestAgedToolImagesMoveTheEarlierRequestOnlyOncePerBlock(t *testing.T) {
	var counts []int
	var prev []provider.Message
	moves := 0
	const rounds = 30
	for range rounds {
		counts = append(counts, 1)
		aged := withAgedToolImages(toolImageMessages(counts...))
		if prev != nil && !reflect.DeepEqual(aged[:len(prev)], prev) {
			moves++
		}
		prev = aged
	}
	if want := (rounds - toolImagesKept) / toolImagesBlock; moves != want {
		t.Fatalf("earlier request bytes moved %d times over %d rounds, want %d", moves, rounds, want)
	}
}

func TestImageTokenEstimateReadsTheHeader(t *testing.T) {
	if got := imageTokenEstimate(pngDataURL(t, 1500, 1000)); got != 2000 {
		t.Fatalf("1500x1000 estimate = %d, want 2000", got)
	}
	bound := int64(visionimage.MaxDim*visionimage.MaxDim+749) / 750
	for _, unreadable := range []string{"data:image/png;base64,!!!!", "img0", ""} {
		if got := imageTokenEstimate(unreadable); got != bound {
			t.Errorf("unreadable %q estimate = %d, want the largest image's %d", unreadable, got, bound)
		}
	}
}

func TestRequestImageTokensCountOnlyWhatTheRequestCarries(t *testing.T) {
	url := pngDataURL(t, 750, 10)
	msgs := toolImageMessages(1, 1, 1, 1, 1, 1, 1)
	for i := range msgs {
		for j := range msgs[i].Images {
			msgs[i].Images[j] = url
		}
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Images: []string{url}},
		provider.Message{Role: provider.RoleTool, LocalOnly: true, Images: []string{url}})
	if got := requestImageTokens(msgs); got != 5*10 {
		t.Fatalf("image tokens = %d, want four kept tool images and one user image", got)
	}
}

func TestCalibrationDeclinesARequestThatCarriedImages(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	text := requestCalibrationShape{requestChars: 4000}
	a.window().setPromptTokenCalibration(1000, text)
	pictured := text
	pictured.imageTokens = 3000
	a.window().setPromptTokenCalibration(4000, pictured)
	if cal := a.sess.output.promptCalibration.Load(); cal == nil || cal.promptTokens != 1000 {
		t.Fatalf("calibration learned from a request billed for pixels: %+v", cal)
	}
	if got := a.window().estimatedShapeTokens(pictured); got != 1000+3000 {
		t.Fatalf("estimate = %d, want calibrated text plus the image estimate", got)
	}
}

// Through the run loop: every request stays inside the image budget while the
// transcript keeps every screenshot the tool returned.
func TestRunKeepsRequestsInsideTheToolImageBudget(t *testing.T) {
	url := pngDataURL(t, 40, 30)
	reg := tool.NewRegistry()
	reg.Add(&fakeImageTool{text: "[image: image/png]", images: []string{url}})
	var turns [][]provider.Chunk
	const shots = 10
	for i := range shots {
		turns = append(turns, []provider.Chunk{toolCallChunk("c"+string(rune('a'+i)), "shot", `{}`), {Type: provider.ChunkDone}})
	}
	turns = append(turns, []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}})
	prov := &scriptedProvider{name: "p", turns: turns}
	a := New(prov, reg, sessionstore.NewSession(""), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if err := a.Run(context.Background(), "take screenshots"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) != shots+1 {
		t.Fatalf("requests = %d, want %d", len(prov.requests), shots+1)
	}
	for i, req := range prov.requests {
		sent := len(toolImagesOf(req.Messages))
		if sent > toolImagesKept+toolImagesBlock-1 {
			t.Fatalf("request %d carried %d tool images, over the budget", i, sent)
		}
		if i > 0 && sent == 0 {
			t.Fatalf("request %d dropped the newest screenshot", i)
		}
	}
	last := prov.requests[len(prov.requests)-1].Messages
	if !strings.Contains(provider.ModelMessages(last)[2].Content, "no longer attached") {
		t.Fatal("the model was not told the oldest screenshot left the request")
	}
	if kept := len(toolImagesOf(a.sess.conversation.Messages)); kept != shots {
		t.Fatalf("transcript holds %d screenshots, want all %d", kept, shots)
	}
}
