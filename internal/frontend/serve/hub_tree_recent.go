package serve

import (
	"cmp"
	"slices"
)

type recentTreeRow struct {
	workspace int
	session   *treeSession
}

func recentRows(tree []treeWorkspace) []recentTreeRow {
	count := 0
	for _, ws := range tree {
		count += len(ws.Sessions)
	}
	rows := make([]recentTreeRow, 0, count)
	for i := range tree {
		for j := range tree[i].Sessions {
			rows = append(rows, recentTreeRow{i, &tree[i].Sessions[j]})
		}
	}
	slices.SortFunc(rows, func(a, b recentTreeRow) int {
		if c := cmp.Compare(b.session.LastActiveAt, a.session.LastActiveAt); c != 0 {
			return c
		}
		return cmp.Compare(a.session.Path, b.session.Path)
	})
	return rows
}

// Each project supplies its first offset+limit activity-ordered rows, so none
// beyond those prefixes can precede the global page. Remaining excludes rows
// before the page and counts every matching conversation after it.
func pageRecentTree(tree []treeWorkspace, offset, limit int) {
	rows := recentRows(tree)
	for i := range tree {
		tree[i].Sessions = []treeSession{}
		tree[i].RecentOffset = &offset
	}
	end := len(rows)
	if limit > 0 {
		end = min(end, offset+limit)
	}
	for i, row := range rows {
		if i >= end {
			tree[row.workspace].Remaining++
		} else if i >= offset {
			tree[row.workspace].Sessions = append(tree[row.workspace].Sessions, *row.session)
		}
	}
}
