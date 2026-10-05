// The provider-visible memory index: scope-qualified references for every
// active fact, override annotations for shadowed global guidance.
package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Index returns the provider-visible index for the cached prefix: every live
// fact, scope-qualified, shadowed globals annotated rather than hidden
// (#7995). A fact past its expires_at is left out — automatic recall already
// refuses to serve one, and a line the model can never act on is prefix rent.
func (s Store) Index() string {
	return s.indexAt(time.Now().UTC())
}

func (s Store) indexAt(now time.Time) string {
	memories := s.ListAll()
	live := memories[:0:0]
	for _, m := range memories {
		if FreshnessFor(m, now) != FreshnessExpired {
			live = append(live, m)
		}
	}
	memories = live
	if len(memories) == 0 {
		return ""
	}
	shadowed := map[string]string{} // global fact ID -> winning project reference
	for _, o := range FindOverrides(memories) {
		shadowed[o.Global.ID] = providerMemoryReference(o.Project)
	}
	sort.SliceStable(memories, func(i, j int) bool {
		if memories[i].Name != memories[j].Name {
			return memories[i].Name < memories[j].Name
		}
		return NormalizeFactScope(string(memories[i].Scope)) == FactScopeProject
	})
	var b strings.Builder
	seen := map[string]bool{} // collapse legacy migration duplicates (same qualified ref)
	for _, memory := range memories {
		if ref := providerMemoryReference(memory); seen[ref] {
			continue
		} else {
			seen[ref] = true
		}
		b.WriteString(renderQualifiedIndexLine(memory))
		if winner, ok := shadowed[memory.ID]; ok &&
			NormalizeFactScope(string(memory.Scope)) == FactScopeGlobal {
			b.WriteString(" (overridden by " + winner + ")")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderQualifiedIndexLine is the provider-index variant of renderIndexLine:
// the link is the scope-qualified reference every memory tool accepts, so a
// name collision across scopes can never be misread.
func renderQualifiedIndexLine(m Memory) string {
	marker := ""
	if ResolveActivation(m) == ActivationPinned {
		marker = " pinned"
	}
	ref := providerMemoryReference(m)
	return fmt.Sprintf("- [%s](%s) — [%s/%s%s] %s",
		displayTitle(m.Title, m.Name), ref,
		NormalizeFactScope(string(m.Scope)), NormalizeType(string(m.Type)), marker, oneLine(m.Description))
}
