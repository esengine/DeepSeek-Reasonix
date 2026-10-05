package agent

import (
	"encoding/base64"
	"fmt"
	"image"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/model/visionimage"
)

// A tool image answers for the moment it was taken, so a request carries only
// the newest few. The rest leave in whole blocks: detaching one edits an earlier
// message and costs the provider's prefix cache from there on, and a block moves
// that edit once where one image at a time would move it every round.
const (
	toolImagesKept  = 3
	toolImagesBlock = 3
)

// withAgedToolImages detaches the oldest tool-result images beyond
// toolImagesKept, a whole block at a time, and says so on each message that
// lost one. User images are the user's to withdraw and are never detached.
// The result depends only on msgs, and a second application changes nothing.
func withAgedToolImages(msgs []provider.Message) []provider.Message {
	detach := detachedToolImages(msgs)
	if detach == 0 {
		return msgs
	}
	out := append([]provider.Message(nil), msgs...)
	for i := range out {
		if detach == 0 {
			break
		}
		m := &out[i]
		if !carriesToolImages(*m) {
			continue
		}
		n := min(detach, len(m.Images))
		m.Images = nil
		if rest := msgs[i].Images[n:]; len(rest) > 0 {
			m.Images = append([]string(nil), rest...)
		}
		m.Content += detachedToolImagesNote(n)
		detach -= n
	}
	return out
}

// detachedToolImages is how many of the oldest tool images a request built from
// msgs leaves behind. Growing history only ever raises it, so an image once
// detached stays detached until a fold removes its message.
func detachedToolImages(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		if carriesToolImages(m) {
			total += len(m.Images)
		}
	}
	excess := total - toolImagesKept
	if excess < toolImagesBlock {
		return 0
	}
	return excess - excess%toolImagesBlock
}

func carriesToolImages(m provider.Message) bool {
	return m.Role == provider.RoleTool && !m.LocalOnly && len(m.Images) > 0
}

func detachedToolImagesNote(n int) string {
	return fmt.Sprintf("\n[%d image(s) this result returned are no longer attached: a request keeps only the newest %d tool images. Call the tool again for a current one.]", n, toolImagesKept)
}

// requestImageTokens estimates what the images a request built from msgs would
// carry cost in the window, after aging. No provider says what an image costs
// before usage returns, and width×height/750 errs high for images Fit bounded,
// so a window this sizes folds early rather than overflowing.
func requestImageTokens(msgs []provider.Message) int64 {
	skip := detachedToolImages(msgs)
	var tokens int64
	for _, m := range msgs {
		if m.LocalOnly {
			continue
		}
		images := m.Images
		if m.Role == provider.RoleTool && skip > 0 {
			n := min(skip, len(images))
			images, skip = images[n:], skip-n
		}
		for _, url := range images {
			tokens += imageTokenEstimate(url)
		}
	}
	return tokens
}

// imageTokenEstimate reads only an image's header. One whose size cannot be
// read is priced at the largest image a request may carry.
func imageTokenEstimate(dataURL string) int64 {
	w, h := visionimage.MaxDim, visionimage.MaxDim
	if _, payload, ok := strings.Cut(dataURL, ";base64,"); ok {
		cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
		if err == nil && cfg.Width > 0 && cfg.Height > 0 {
			w, h = cfg.Width, cfg.Height
		}
	}
	return (int64(w)*int64(h) + 749) / 750
}
