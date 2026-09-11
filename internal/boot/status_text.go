package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/extension"
)

// FormatRuntimeStatus renders a doctor-facing explanation of component
// activation state: why a component is Inactive/Failed and what the plan did.
func FormatRuntimeStatus(status *extension.RuntimeStatus) string {
	if status == nil {
		return "runtime status: unavailable\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "published generation: %d\n", status.PublishedGeneration)
	if status.Plan != nil {
		p := status.Plan
		fmt.Fprintf(&b, "plan: from=%d to=%d prefixChanged=%v providerChanged=%v\n", p.FromGeneration, p.ToGeneration, p.PrefixChanged, p.ProviderChanged)
		if len(p.Added) > 0 {
			fmt.Fprintf(&b, "  added: %s\n", joinIDs(p.Added))
		}
		if len(p.Removed) > 0 {
			fmt.Fprintf(&b, "  removed: %s\n", joinIDs(p.Removed))
		}
		if len(p.Reloaded) > 0 {
			fmt.Fprintf(&b, "  reloaded: %s\n", joinIDs(p.Reloaded))
		}
		if len(p.Unchanged) > 0 {
			fmt.Fprintf(&b, "  unchanged: %s\n", joinIDs(p.Unchanged))
		}
	}
	if len(status.Components) == 0 {
		b.WriteString("components: (none)\n")
	} else {
		b.WriteString("components:\n")
		for _, c := range status.Components {
			fmt.Fprintf(&b, "  %s state=%s gen=%d", c.ID, c.State, c.Generation)
			if c.Error != "" {
				fmt.Fprintf(&b, " error=%s", c.Error)
			}
			b.WriteByte('\n')
			for _, d := range c.Diagnostics {
				fmt.Fprintf(&b, "    - %s\n", d)
			}
			if c.State == extension.ComponentInactive && len(c.Diagnostics) == 0 && c.Error == "" {
				b.WriteString("    - not activated (missing required dependency or optional runtime)\n")
			}
		}
	}
	if len(status.Receipts) > 0 {
		b.WriteString("effect receipts:\n")
		for _, r := range status.Receipts {
			fmt.Fprintf(&b, "  %s class=%d owner=%s compensation=%s\n", r.ID, r.Class, r.Owner, r.CompensationStatus)
		}
	}
	m := extension.DefaultLifecycleMetrics.Snapshot()
	fmt.Fprintf(&b, "metrics: publishes=%d drains=%d staleDrops=%d admitReject=%d noOpRebuilds=%d fullRebuilds=%d subgraphRebuilds=%d\n",
		m.Publishes, m.Drains, m.StaleDrops, m.AdmissionRejected, m.NoOpRebuilds, m.FullRebuilds, m.SubgraphRebuilds)
	return b.String()
}

func joinIDs(ids []extension.ComponentID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}
