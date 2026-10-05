package cli

import (
	"strings"
)

const ()

// compItem is one menu row: label shown, insert applied on accept, hint dimmed.
// descend marks a directory entry — accepting it fills the input and re-opens
// the menu one level deeper instead of closing.
type compItem struct {
	label   string
	insert  string
	hint    string
	descend bool
}

const ()

func renameSlashItem(items []compItem, oldLabel, newLabel string) []compItem {
	if oldLabel == newLabel {
		return items
	}
	for i := range items {
		if items[i].label != oldLabel {
			continue
		}
		items[i].label = newLabel
		if after, ok := strings.CutPrefix(items[i].insert, oldLabel); ok {
			items[i].insert = newLabel + after
		}
		break
	}
	return items
}

func removeSlashItems(items []compItem, label string) []compItem {
	out := make([]compItem, 0, len(items))
	for _, item := range items {
		if item.label != label {
			out = append(out, item)
		}
	}
	return out
}
