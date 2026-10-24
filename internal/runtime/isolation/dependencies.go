package isolation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

const CodeDependentResults = "isolation.dependent_results"

// ResultLineage pins a direct parent until every registered child is settled.
// Its owner updates the durable record under the parent's entry lock.
type ResultLineage struct {
	Parent    *ParentResult          `json:"parent,omitempty"`
	Admission string                 `json:"admission"`
	Children  map[string]ChildResult `json:"children,omitempty"`
}

type ParentResult struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	Record     string `json:"record"`
	Token      string `json:"token"`
}

type ChildResult struct {
	ID          string `json:"id"`
	Root        string `json:"root"`
	Workspace   string `json:"workspace"`
	Token       string `json:"token"`
	Preparation string `json:"preparation,omitempty"`
}

// BeginChild registers the dependency before reading the parent's snapshot.
// Failed preparation retains its registration whenever a child record exists.
func (s *Store) BeginChild(ctx context.Context, parent *Entry, children *Store, owner string) (*Entry, error) {
	return s.beginChildUsing(ctx, parent, children, owner, func(id string, lineage *ResultLineage) (*Entry, error) {
		return children.beginOwned(ctx, children.workspace, owner, id, lineage)
	})
}

func (s *Store) CopyChild(ctx context.Context, parent *Entry, children *Store, source CopySource, owner string) (*Entry, error) {
	return s.beginChildUsing(ctx, parent, children, owner, func(id string, lineage *ResultLineage) (*Entry, error) {
		return children.copyOwned(ctx, source, owner, id, lineage)
	})
}

func (s *Store) beginChildUsing(ctx context.Context, parent *Entry, children *Store, owner string, begin func(string, *ResultLineage) (*Entry, error)) (*Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateChildTarget(parent, children, owner); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	release, err := children.holdChildPreparation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	link := &ParentResult{ID: parent.ID, Generation: parent.RunGeneration, Record: entryRecordPath(parent), Token: rand.Text()}
	registration := ChildResult{ID: id, Root: children.managedRoot, Workspace: children.workspace, Token: link.Token, Preparation: childPreparationV1}
	if err := s.registerChild(ctx, parent, registration); err != nil {
		return nil, err
	}
	if s.childRegistrationProbe != nil {
		s.childRegistrationProbe(parent, registration)
	}
	child, err := begin(id, &ResultLineage{Parent: link, Admission: "open"})
	if err != nil {
		err = errors.Join(err, s.retractUnpreparedChild(context.WithoutCancel(ctx), parent, children, id, link.Token))
	}
	return child, err
}

func (s *Store) registerChild(ctx context.Context, parent *Entry, child ChildResult) error {
	unlock, err := s.lockEntry(ctx, parent.ID)
	if err != nil {
		return err
	}
	defer unlock()
	d, raw, err := s.childAdmission(parent)
	if err != nil {
		return err
	}
	lineage := d.Lineage
	if lineage == nil {
		lineage = &ResultLineage{Admission: "open"}
	}
	if lineage.Children == nil {
		lineage.Children = map[string]ChildResult{}
	}
	if _, exists := lineage.Children[child.ID]; exists {
		return tool.Refusal{Code: CodeOwnerMismatch, Message: "child identity is already registered; original dependency retained"}
	}
	lineage.Children[child.ID] = child
	return saveLineage(entryRecordPath(parent), raw, lineage)
}

func (s *Store) retractUnpreparedChild(ctx context.Context, parent *Entry, children *Store, id, token string) error {
	unlock, err := s.lockEntry(ctx, parent.ID)
	if err != nil {
		return err
	}
	defer unlock()
	d, raw, err := readLineageRecord(entryRecordPath(parent))
	if err != nil {
		return err
	}
	if d.RunGeneration != parent.RunGeneration || d.Lineage == nil || d.Lineage.Children[id].Token != token {
		return tool.Refusal{Code: CodeStaleExecution, Message: "child reservation changed; dependency retained"}
	}
	present, err := children.containsResultID(id)
	if err != nil || present {
		return err
	}
	delete(d.Lineage.Children, id)
	return saveLineage(entryRecordPath(parent), raw, d.Lineage)
}

// SealChildren closes admission before the parent tries to publish a manifest.
// Unsettled results retain the parent's files and generation for recovery.
func (s *Store) SealChildren(ctx context.Context, parent *Entry) error {
	unlock, err := s.lockEntry(ctx, parent.ID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.checkRunGeneration(parent); err != nil {
		return err
	}
	return s.sealChildrenLocked(parent)
}

func (s *Store) sealChildrenLocked(parent *Entry) error {
	d, raw, err := readLineageRecord(entryRecordPath(parent))
	if err != nil {
		return err
	}
	lineage := d.Lineage
	if lineage == nil {
		lineage = &ResultLineage{}
	}
	lineage.Admission = "sealed"
	if err := saveLineage(entryRecordPath(parent), raw, lineage); err != nil {
		return err
	}
	parent.Lineage = lineage
	return s.requireSettledChildren(parent)
}

func (s *Store) requireSettledChildren(parent *Entry) error {
	if parent.Lineage == nil {
		return nil
	}
	for id, child := range parent.Lineage.Children {
		children := NewStore(child.Root, child.Workspace, parent.Cand.Repo)
		d, err := children.managedResultMetadata(id)
		var refusal tool.Refusal
		if errors.As(err, &refusal) && refusal.Code == CodeUnknownID {
			return dependentResults(fmt.Sprintf("child reservation %s needs recovery; parent retained", id))
		}
		if err != nil {
			return err
		}
		if d.Workspace != child.Workspace || d.Lineage == nil || !matchesParentResult(d.Lineage.Parent, parent, child.Token) || !terminalState(d.State) || !d.CleanupComplete {
			return dependentResults(fmt.Sprintf("child result %s is not settled; parent retained", id))
		}
		release, err := filelock.TryAcquire(children.runLock(id))
		if err != nil {
			return dependentResults("child execution is still active; parent retained")
		}
		release()
		if err := children.requireSettledIntegrations(d.entry()); err != nil {
			return err
		}
	}
	return nil
}

func dependentResults(message string) error {
	return tool.Refusal{Code: CodeDependentResults, Message: message}
}

func readLineageRecord(path string) (durableEntry, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return durableEntry{}, nil, err
	}
	var d durableEntry
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, raw, err
	}
	if !supportedResultVersion(d) || !supportedLifecycle(d) {
		return d, raw, tool.Refusal{Code: CodeUnsupportedState, Message: "unsupported child dependency protocol; record retained"}
	}
	return d, raw, nil
}

func saveLineage(path string, raw []byte, lineage *ResultLineage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	next, err := json.Marshal(lineage)
	if err != nil {
		return err
	}
	if previous := fields["lineage"]; len(previous) != 0 {
		var oldFields map[string]json.RawMessage
		if err := json.Unmarshal(previous, &oldFields); err != nil {
			return err
		}
		next, err = mergeIdentityFields(previous, next)
		if err != nil {
			return err
		}
		var merged map[string]json.RawMessage
		if err := json.Unmarshal(next, &merged); err != nil {
			return err
		}
		var children map[string]json.RawMessage
		if old := oldFields["children"]; len(old) != 0 {
			if err := json.Unmarshal(old, &children); err != nil {
				return err
			}
		}
		retained := map[string]json.RawMessage{}
		for id, child := range lineage.Children {
			body, err := json.Marshal(child)
			if err != nil {
				return err
			}
			if previous := children[id]; len(previous) != 0 {
				body, err = mergeIdentityFields(previous, body)
				if err != nil {
					return err
				}
			}
			retained[id] = body
		}
		merged["children"], err = json.Marshal(retained)
		if err != nil {
			return err
		}
		next, err = json.Marshal(merged)
		if err != nil {
			return err
		}
	}
	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil {
		return err
	}
	if version < 4 {
		fields["version"] = json.RawMessage("4")
	}
	fields["lineage"] = next
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, body, 0600)
}

func (s *Store) reopenChildren(parent *Entry) error {
	if parent.Lineage == nil {
		return nil
	}
	d, raw, err := readLineageRecord(entryRecordPath(parent))
	if err != nil {
		return err
	}
	if err := s.requireSettledChildren(d.entry()); err != nil {
		return err
	}
	d.Lineage.Children, d.Lineage.Admission = nil, "open"
	if err := saveLineage(entryRecordPath(parent), raw, d.Lineage); err != nil {
		return err
	}
	parent.Lineage = d.Lineage
	return nil
}

func supportedLineage(lineage *ResultLineage) bool {
	if lineage == nil {
		return true
	}
	if lineage.Admission != "open" && lineage.Admission != "sealed" {
		return false
	}
	if p := lineage.Parent; p != nil && (!validID(p.ID) || p.Generation == "" || p.Token == "" || !filepath.IsAbs(p.Record)) {
		return false
	}
	for id, child := range lineage.Children {
		if !validID(id) || child.ID != id || !filepath.IsAbs(child.Root) || !filepath.IsAbs(child.Workspace) || child.Token == "" ||
			(child.Preparation != "" && child.Preparation != childPreparationV1) {
			return false
		}
	}
	return true
}
