package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ImageOffloadTarget names image slots inside one message. Indexes count every
// slot, including previously offloaded ones.
type ImageOffloadTarget struct {
	MessageID    string `json:"messageId"`
	ImageIndexes []int  `json:"imageIndexes"`
}

// ImageOffloadPayload is the durable image/offload event body.
type ImageOffloadPayload struct {
	Targets []ImageOffloadTarget `json:"targets"`
}

// ApplyImageOffload replaces selected input-image slots on a copy of messages.
// UI history is not modified by this helper.
func ApplyImageOffload(messages []Message, targets []ImageOffloadTarget) []Message {
	if len(messages) == 0 || len(targets) == 0 {
		return messages
	}
	out := append([]Message(nil), messages...)
	byID := map[string]int{}
	for i := range out {
		if out[i].ID != "" {
			byID[out[i].ID] = i
		}
	}
	for _, target := range targets {
		idx, ok := byID[target.MessageID]
		if !ok {
			continue
		}
		msg := out[idx]
		if len(msg.Images) == 0 {
			continue
		}
		images := append([]string(nil), msg.Images...)
		changed := false
		for _, imageIdx := range target.ImageIndexes {
			if imageIdx < 0 || imageIdx >= len(images) {
				continue
			}
			if images[imageIdx] == ImageOffloadedRef {
				continue
			}
			images[imageIdx] = ImageOffloadedRef
			changed = true
		}
		if !changed {
			continue
		}
		msg.Images = images
		if msg.Role != RoleAssistant && !strings.Contains(msg.Content, OffloadedImageNotice()) {
			if msg.Content == "" {
				msg.Content = OffloadedImageNotice()
			} else {
				msg.Content = msg.Content + "\n" + OffloadedImageNotice()
			}
		}
		out[idx] = msg
	}
	return out
}

// OffloadOldestImages names the oldest still-retained input images to omit.
// Assistant output images are ignored. Indexes count every slot, including
// previously offloaded ones.
func OffloadOldestImages(messages []Message, count int) []ImageOffloadTarget {
	if count <= 0 {
		return nil
	}
	type occ struct {
		messageID string
		index     int
	}
	var retained []occ
	for _, msg := range messages {
		if msg.Role == RoleAssistant || msg.ID == "" {
			continue
		}
		for i, ref := range msg.Images {
			if ClassifyImage(ref) == ImageNone {
				continue
			}
			retained = append(retained, occ{messageID: msg.ID, index: i})
			if len(retained) == count {
				break
			}
		}
		if len(retained) == count {
			break
		}
	}
	if len(retained) == 0 {
		return nil
	}
	byID := map[string]*ImageOffloadTarget{}
	var order []string
	for _, item := range retained {
		cur, ok := byID[item.messageID]
		if !ok {
			cur = &ImageOffloadTarget{MessageID: item.messageID}
			byID[item.messageID] = cur
			order = append(order, item.messageID)
		}
		cur.ImageIndexes = append(cur.ImageIndexes, item.index)
	}
	out := make([]ImageOffloadTarget, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func MergeImageOffload(existing, extra []ImageOffloadTarget) []ImageOffloadTarget {
	if len(extra) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return append([]ImageOffloadTarget(nil), extra...)
	}
	byID := map[string]map[int]bool{}
	var order []string
	add := func(targets []ImageOffloadTarget) {
		for _, t := range targets {
			seen, ok := byID[t.MessageID]
			if !ok {
				seen = map[int]bool{}
				byID[t.MessageID] = seen
				order = append(order, t.MessageID)
			}
			for _, idx := range t.ImageIndexes {
				seen[idx] = true
			}
		}
	}
	add(existing)
	add(extra)
	out := make([]ImageOffloadTarget, 0, len(order))
	for _, id := range order {
		seen := byID[id]
		out = append(out, ImageOffloadTarget{MessageID: id, ImageIndexes: uniqueSorted(seen)})
	}
	return out
}

func uniqueSorted(seen map[int]bool) []int {
	maxIdx := -1
	for idx := range seen {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	out := make([]int, 0, len(seen))
	for i := 0; i <= maxIdx; i++ {
		if seen[i] {
			out = append(out, i)
		}
	}
	return out
}

func (p ImageOffloadPayload) Marshal() (json.RawMessage, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("image offload payload: %w", err)
	}
	return raw, nil
}
