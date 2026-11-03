package memory

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	usageFile         = ".usage.json"
	reinforceFull     = 1.8
	reinforceWeak     = 1.2
	maxStrengthDays   = 365.0
	RetentionClearMin = 0.6
	RetentionFadedMax = 0.2
	linkWeightMin     = 2.0
	maxNeighbors      = 2
)

type usageRecord struct {
	Strength   float64   `json:"strength,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	Uses       int       `json:"uses,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at,omitempty"`
}

type usageData struct {
	Facts map[string]usageRecord `json:"facts,omitempty"`
	Links map[string]float64     `json:"links,omitempty"`
}

func readUsage(dir string) usageData {
	data := usageData{Facts: map[string]usageRecord{}, Links: map[string]float64{}}
	if dir == "" {
		return data
	}
	raw, err := os.ReadFile(filepath.Join(dir, usageFile))
	if err != nil {
		return data
	}
	var parsed usageData
	if json.Unmarshal(raw, &parsed) != nil {
		return data
	}
	maps.Copy(data.Facts, parsed.Facts)
	for key, w := range parsed.Links {
		if w > 0 && !math.IsNaN(w) && !math.IsInf(w, 0) {
			data.Links[key] = w
		}
	}
	return data
}

func writeUsage(dir string, data usageData) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".usage.*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, filepath.Join(dir, usageFile))
}

func (s Store) usageDirFor(m Memory) string {
	if NormalizeFactScope(string(m.Scope)) == FactScopeGlobal && s.GlobalDir != "" {
		return s.GlobalDir
	}
	return s.Dir
}

func applyUsage(memories []Memory, byDir map[string]usageData, dirOf func(Memory) string) {
	for i := range memories {
		rec, ok := byDir[dirOf(memories[i])].Facts[memories[i].ID]
		if !ok {
			continue
		}
		if rec.Strength > 0 {
			memories[i].Strength = rec.Strength
		}
		if !rec.LastUsedAt.IsZero() {
			memories[i].LastUsedAt = rec.LastUsedAt
		}
		if rec.Uses > 0 {
			memories[i].Uses = rec.Uses
		}
		if !rec.ReviewedAt.IsZero() {
			memories[i].ReviewedAt = rec.ReviewedAt
		}
	}
}

func (s Store) withUsage(memories []Memory) []Memory {
	byDir := map[string]usageData{}
	for _, dir := range s.dirs() {
		if dir != "" {
			byDir[dir] = readUsage(dir)
		}
	}
	applyUsage(memories, byDir, s.usageDirFor)
	return memories
}

func (s Store) links() map[string]float64 {
	out := map[string]float64{}
	for _, dir := range s.dirs() {
		if dir == "" {
			continue
		}
		for key, w := range readUsage(dir).Links {
			out[key] += w
		}
	}
	return out
}

func linkKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func (s Store) RecordUse(used []Memory, weak bool, now time.Time) error {
	if s.Dir == "" && s.GlobalDir == "" || len(used) == 0 {
		return nil
	}
	release, err := s.lockMutations()
	if err != nil {
		return err
	}
	defer release()
	factor := reinforceFull
	if weak {
		factor = reinforceWeak
	}
	byDir := map[string]usageData{}
	load := func(dir string) usageData {
		if d, ok := byDir[dir]; ok {
			return d
		}
		d := readUsage(dir)
		byDir[dir] = d
		return d
	}
	seen := map[string]bool{}
	var ids []string
	for _, m := range used {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		ids = append(ids, m.ID)
		dir := s.usageDirFor(m)
		data := load(dir)
		rec := data.Facts[m.ID]
		current := rec.Strength
		if current <= 0 {
			current = m.StrengthDays()
		}
		if !m.NoDecay() {
			rec.Strength = math.Min(current*factor, maxStrengthDays)
		}
		rec.LastUsedAt = now.UTC()
		rec.Uses++
		data.Facts[m.ID] = rec
	}
	if len(ids) > 1 && s.Dir != "" {
		sort.Strings(ids)
		data := load(s.Dir)
		for i := range ids {
			for j := i + 1; j < len(ids); j++ {
				data.Links[linkKey(ids[i], ids[j])]++
			}
		}
	}
	for dir, data := range byDir {
		if dir == "" {
			continue
		}
		if err := writeUsage(dir, data); err != nil {
			return err
		}
	}
	return nil
}

const fadedWeightFloor = 0.35

func retentionWeight(r float64) float64 {
	if r >= RetentionClearMin {
		return 1
	}
	return math.Max(r/RetentionClearMin, fadedWeightFloor)
}

func (m Memory) retentionAnchor() time.Time {
	ref := m.LastUsedAt
	for _, t := range []time.Time{m.LastVerifiedAt, m.UpdatedAt, m.CreatedAt} {
		if t.After(ref) {
			ref = t
		}
	}
	return ref
}

func (m Memory) Retention(now time.Time) float64 {
	if m.NoDecay() {
		return 1
	}
	ref := m.retentionAnchor()
	if ref.IsZero() {
		return 1
	}
	days := now.Sub(ref).Hours() / 24
	if days <= 0 {
		return 1
	}
	return math.Exp(-days / m.StrengthDays())
}

type RetentionState string

const (
	RetentionClear RetentionState = "clear"
	RetentionFuzzy RetentionState = "fuzzy"
	RetentionFaded RetentionState = "faded"
)

func (m Memory) RetentionState(now time.Time) RetentionState {
	switch r := m.Retention(now); {
	case r >= RetentionClearMin:
		return RetentionClear
	case r >= RetentionFadedMax:
		return RetentionFuzzy
	}
	return RetentionFaded
}

func neighborsOf(seeds []RecallHit, docs []autoRecallDoc, links map[string]float64, now time.Time) []RecallHit {
	if len(links) == 0 || len(seeds) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, h := range seeds {
		have[h.Memory.ID] = true
	}
	type cand struct {
		doc autoRecallDoc
		w   float64
	}
	var cands []cand
	for _, doc := range docs {
		id := doc.memory.ID
		if have[id] || memoryFreshness(doc.memory, now) == FreshnessExpired {
			continue
		}
		best := 0.0
		for i, seed := range seeds {
			if i >= galaxySeeds {
				break
			}
			if w := links[linkKey(seed.Memory.ID, id)]; w > best {
				best = w
			}
		}
		if best >= linkWeightMin {
			cands = append(cands, cand{doc, best})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].w != cands[j].w {
			return cands[i].w > cands[j].w
		}
		return cands[i].doc.memory.ID < cands[j].doc.memory.ID
	})
	var out []RecallHit
	for _, c := range cands {
		if len(out) >= maxNeighbors {
			break
		}
		out = append(out, RecallHit{Memory: c.doc.memory, Freshness: memoryFreshness(c.doc.memory, now), Reason: "often used together", Snippet: strings.TrimSpace(c.doc.memory.Description)})
	}
	return out
}

const newFactWindow = 14 * 24 * time.Hour

var ErrNoSuchFact = errors.New("memory.not_found")

func (m Memory) AwaitingReview(now time.Time) bool {
	return m.ReviewedAt.IsZero() && !m.CreatedAt.IsZero() && now.Sub(m.CreatedAt) < newFactWindow
}

func (m Memory) ClearForDays(now time.Time) int {
	if m.NoDecay() {
		return 0
	}
	ref := m.retentionAnchor()
	if ref.IsZero() {
		ref = now
	}
	left := m.StrengthDays()*math.Log(1/RetentionClearMin) - now.Sub(ref).Hours()/24
	if left <= 0 {
		return 0
	}
	return int(math.Ceil(left))
}

func (s Store) MarkReviewed(m Memory, now time.Time) error {
	if (s.Dir == "" && s.GlobalDir == "") || m.ID == "" {
		return nil
	}
	release, err := s.lockMutations()
	if err != nil {
		return err
	}
	defer release()
	dir := s.usageDirFor(m)
	data := readUsage(dir)
	rec := data.Facts[m.ID]
	rec.ReviewedAt = now.UTC()
	data.Facts[m.ID] = rec
	return writeUsage(dir, data)
}
