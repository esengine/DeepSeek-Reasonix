package browser

import (
	"fmt"
	"strings"
	"sync"
)

// refTable names elements for the model. A ref is issued once and never
// reused, so one the model read earlier cannot come to mean a different
// element; the same element keeps its ref across snapshots of one document.
type refTable struct {
	mu     sync.Mutex
	next   int
	byRef  map[string]refTarget
	byNode map[refKey]string
}

type refKey struct {
	tab  string
	node int64
}

type refTarget struct {
	tab     string
	node    int64
	retired bool
}

func (r *refTable) refFor(tab string, node int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byRef == nil {
		r.byRef, r.byNode = map[string]refTarget{}, map[refKey]string{}
	}
	key := refKey{tab, node}
	if ref, ok := r.byNode[key]; ok {
		return ref
	}
	r.next++
	ref := fmt.Sprintf("e%d", r.next)
	r.byNode[key] = ref
	r.byRef[ref] = refTarget{tab: tab, node: node}
	return ref
}

func (r *refTable) resolve(ref string) (refTarget, error) {
	ref = strings.TrimSpace(ref)
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.byRef[ref]
	switch {
	case !ok:
		return refTarget{}, &Failure{Code: CodeUnknownRef, Ref: ref, Detail: fmt.Sprintf("%s was never issued; refs come from a snapshot", ref)}
	case target.retired:
		return refTarget{}, &Failure{Code: CodeStaleRef, Ref: ref, Detail: fmt.Sprintf("%s belonged to a page that has since navigated away; take a new snapshot", ref)}
	}
	return target, nil
}

// retireTab ends every ref issued on a tab's current document.
func (r *refTable) retireTab(tab string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, ref := range r.byNode {
		if key.tab != tab {
			continue
		}
		delete(r.byNode, key)
		target := r.byRef[ref]
		target.retired = true
		r.byRef[ref] = target
	}
}
