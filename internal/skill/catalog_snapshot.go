package skill

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

func (s *Store) discoverSkillsUncached(ctx context.Context) ([]Skill, map[string]Skill) {
	if s == nil || s.disableDiscovery {
		return nil, nil
	}
	var out []Skill
	var builtinEntries []Skill
	if !s.disableBuiltins {
		builtinEntries = applyBuiltinSkillUpdates(builtinSkills(), s.reasonixHomeDir)
	}
	for _, r := range s.roots() {
		if ctx.Err() != nil {
			return nil, nil
		}
		if r.Status != StatusOK {
			continue
		}
		for _, sk := range s.discoverRoot(ctx, r) {
			if s.disabledName(sk.Name) {
				continue
			}
			if len(r.plugins) == 0 {
				out = append(out, sk)
				continue
			}
			for _, plugin := range r.plugins {
				owned := sk
				owned.Plugin = plugin
				if r.forceSubagent {
					owned.SlashPrefix = plugin + ":agent"
				}
				out = append(out, owned)
			}
		}
	}
	if !s.disableBuiltins {
		for _, sk := range builtinEntries {
			if !s.disabledName(sk.Name) {
				out = append(out, skillCandidate(sk))
			}
		}
	}
	builtins := map[string]Skill{}
	if !s.disableBuiltins {
		for _, sk := range builtinEntries {
			if !s.disabledName(sk.Name) {
				builtins[sk.Name] = sk
			}
		}
	}
	return out, builtins
}

func skillCandidate(skill Skill) Skill {
	skill.Body = ""
	return skill
}

func enabledFromDiscovered(discovered []Skill) ([]Skill, map[string]Skill) {
	byName := map[string]Skill{}
	for _, sk := range discovered {
		if _, dup := byName[sk.Name]; !dup {
			byName[sk.Name] = sk
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, byName
}

func cloneSkills(in []Skill) []Skill {
	out := make([]Skill, len(in))
	for i, sk := range in {
		out[i] = sk
		out[i].AllowedTools = append([]string(nil), sk.AllowedTools...)
		out[i].Triggers = append([]string(nil), sk.Triggers...)
		out[i].NegativeTriggers = append([]string(nil), sk.NegativeTriggers...)
		out[i].Requires = append([]string(nil), sk.Requires...)
		out[i].Profiles = append([]string(nil), sk.Profiles...)
		out[i].InvalidProfiles = append([]string(nil), sk.InvalidProfiles...)
	}
	return out
}

func cloneSkill(sk Skill) Skill { return cloneSkills([]Skill{sk})[0] }

func (s *Store) buildCatalog(ctx context.Context, generation uint64) {
	discovered, builtins := s.discoverSkillsUncached(ctx)
	if ctx.Err() != nil {
		s.catalogMu.Lock()
		if s.catalogFlight != nil && s.catalogFlight.generation == generation {
			close(s.catalogFlight.done)
			s.catalogFlight = nil
		}
		s.catalogMu.Unlock()
		return
	}
	enabled, byName := enabledFromDiscovered(discovered)
	built := &catalogSnapshot{
		version: generation, rootSig: s.rootSignature(), discovered: cloneSkills(discovered), enabled: cloneSkills(enabled),
		byName: byName, slash: VisibleSlashSkills(discovered), builtins: builtins,
	}
	s.catalogMu.Lock()
	if s.catalogGen == generation {
		s.catalog = built
	}
	if s.catalogFlight != nil && s.catalogFlight.generation == generation {
		close(s.catalogFlight.done)
		s.catalogFlight = nil
	}
	s.catalogMu.Unlock()
}

func (s *Store) rootSignature() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	for _, root := range s.roots() {
		fmt.Fprintf(&b, "%s\x00%s\x00", root.Dir, root.Status)
		if info, err := os.Stat(root.Dir); err == nil {
			fmt.Fprintf(&b, "%d\x00%d\x00", info.ModTime().UnixNano(), info.Size())
		}
	}
	return b.String()
}

func (s *Store) invalidateChangedRoots() {
	if s == nil {
		return
	}
	sig := s.rootSignature()
	s.catalogMu.Lock()
	if s.catalog != nil && s.catalog.rootSig != sig {
		s.catalogGen++
	}
	s.catalogMu.Unlock()
}

// Snapshot returns one immutable catalog generation. Concurrent cold callers
// share one scan. A cancelled waiter does not cancel that shared scan; when an
// older complete snapshot exists it is returned explicitly marked stale.
func (s *Store) Snapshot(ctx context.Context) (CatalogSnapshot, error) {
	if s == nil {
		return CatalogSnapshot{Complete: true}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CatalogSnapshot{}, err
	}
	if s.autoWatch {
		s.ensureWatcher()
	}
	// One initial discovery plus at most two retries when invalidation races
	// publication. Persistent churn returns the last complete generation.
	for range 3 {
		s.catalogMu.Lock()
		generation := s.catalogGen
		if s.catalog != nil && s.catalog.version == generation {
			snapshot := CatalogSnapshot{Version: generation, Complete: true, Candidates: cloneSkills(s.catalog.enabled)}
			s.catalogMu.Unlock()
			return snapshot, nil
		}
		stale := s.catalog
		flight := s.catalogFlight
		if flight == nil {
			scanCtx, cancel := context.WithCancel(context.Background())
			flight = &catalogFlight{generation: generation, done: make(chan struct{}), cancel: cancel}
			s.catalogFlight = flight
			s.discoveryScans++
			go s.buildCatalog(scanCtx, generation)
		}
		done := flight.done
		s.catalogMu.Unlock()
		select {
		case <-ctx.Done():
			if stale != nil {
				return CatalogSnapshot{Version: stale.version, Complete: false, Stale: true, Candidates: cloneSkills(stale.enabled)}, nil
			}
			return CatalogSnapshot{}, ctx.Err()
		case <-done:
		}
	}
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	if s.catalog != nil {
		return CatalogSnapshot{Version: s.catalog.version, Complete: false, Stale: true, Candidates: cloneSkills(s.catalog.enabled)}, nil
	}
	return CatalogSnapshot{}, fmt.Errorf("skill catalog changed during all three discovery attempts")
}

// Close releases this store's directory subscriptions. It is idempotent; a
// closed store remains readable from its last complete snapshot but performs
// no further automatic invalidation.
func (s *Store) Invalidate(_ string) {
	if s == nil {
		return
	}
	s.catalogMu.Lock()
	s.catalogGen++
	s.catalogMu.Unlock()
}

// DiscoveryScans exposes the deterministic scan count for diagnostics and
// complexity tests. Warm reads leave it unchanged.
func (s *Store) DiscoveryScans() uint64 {
	if s == nil {
		return 0
	}
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.discoveryScans
}

func (s *Store) catalogSnapshot() *catalogSnapshot {
	_, _ = s.Snapshot(context.Background())
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.catalog
}

func (s *Store) discoveredSkills() []Skill {
	s.invalidateChangedRoots()
	snapshot := s.catalogSnapshot()
	if snapshot == nil {
		return nil
	}
	return cloneSkills(snapshot.discovered)
}

func (s *Store) enabledSkills() []Skill {
	s.invalidateChangedRoots()
	snapshot := s.catalogSnapshot()
	if snapshot == nil {
		return nil
	}
	return cloneSkills(snapshot.enabled)
}

// List returns every model-visible skill, deduped by its bare internal name
// (first/highest-priority root wins) and sorted for a cache-stable index.
// Role-setting profiles do not filter this surface.
