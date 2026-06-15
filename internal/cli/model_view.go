package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/i18n"
)

func renderModels(width int, refs []string, active string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("%s", i18n.M.ModelListHeader))
	for _, ref := range refs {
		status := ""
		if ref == active {
			status = "  " + viewStatus("active")
		}
		fmt.Fprintf(&b, "  %s%s\n", viewCompactText(ref, viewBudget(width, 2+visibleWidth(status))), status)
	}
	b.WriteString(viewHint(viewCompactText("switch with /model <provider/model>", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}
