package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/i18n"
	"reasonix/internal/memory"
)

func renderMemory(width int, set *memory.Set) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("%s", strings.TrimRight(i18n.M.MemoryLoaded, ":：")))
	if len(set.Docs) > 0 {
		b.WriteString(viewSubhead("docs") + "\n")
		for _, d := range set.Docs {
			scope := "(" + string(d.Scope) + ")"
			fmt.Fprintf(&b, "  %s  %s\n", viewMeta(scope), viewCompactPath(d.Path, viewBudget(width, 2+visibleWidth(scope)+2)))
		}
	}
	facts := set.Store.List()
	archived := set.Store.ListArchived()
	hasSaved := len(facts) > 0 || strings.TrimSpace(set.Index) != ""
	if hasSaved {
		if len(set.Docs) > 0 {
			b.WriteByte('\n')
		}
		header := strings.TrimRight(strings.TrimSpace(i18n.M.MemorySavedHeader), ":：")
		b.WriteString(viewSubhead(viewCompactText(header, viewBudget(width, 2))) + "\n")
		for _, f := range facts {
			label := f.Title
			if label == "" {
				label = f.Description
			}
			meta := ""
			if label != "" {
				meta = "  " + viewMeta(viewCompactText(label, min(40, viewBudget(width, 2+visibleWidth(f.Name)+2))))
			}
			fmt.Fprintf(&b, "  %s%s\n", viewCompactText(f.Name, viewBudget(width, 2+visibleWidth(meta))), meta)
		}
	}
	if len(archived) > 0 {
		if len(set.Docs) > 0 || hasSaved {
			b.WriteByte('\n')
		}
		b.WriteString(viewSubhead(viewCompactText(i18n.M.ListMemoryArchived, viewBudget(width, 2))) + "\n")
		for _, f := range archived {
			meta := string(f.Type)
			if !f.ArchivedAt.IsZero() {
				meta += " · " + f.ArchivedAt.Format("2006-01-02 15:04:05Z")
			}
			name := viewCompactText(f.Name, viewBudget(width, 2))
			fmt.Fprintf(&b, "  %s  %s\n", name, viewMeta(viewCompactText(meta, min(48, viewBudget(width, 2+visibleWidth(name)+2)))))
			fmt.Fprintf(&b, "    %s\n", viewCompactPath(f.Path, viewBudget(width, 4)))
		}
	}
	if (hasSaved || len(archived) > 0) && set.Store.Dir != "" {
		fmt.Fprintf(&b, "  %s\n", viewCompactText(strings.TrimSpace(fmt.Sprintf(i18n.M.MemoryStoredUnderFmt, set.Store.Dir)), viewBudget(width, 2)))
	}
	b.WriteString("\n")
	b.WriteString(viewHint(viewCompactText(i18n.M.MemoryEditHint, viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}
