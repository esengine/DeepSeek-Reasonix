package main

import (
	"fmt"
	"sort"
)

type class string

const (
	classKeepBusiness class = "keep-business"
	classMigrateHost  class = "migrate-host"
	classDeleteShell  class = "delete-shell"
)

// entry is one desktop shell entry point with its migration destination.
type entry struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
	Location string `json:"location,omitempty"`
	Class    class  `json:"class"`
	Owner    string `json:"owner"`
}

const (
	kindCommand        = "command"
	kindNativeCall     = "native-call"
	kindEvent          = "event"
	kindFrontendNative = "frontend-native"
	kindFrontendEvent  = "frontend-event"
	kindCSSMarker      = "css-marker"
	kindPersistence    = "persistence"
	kindShellFile      = "shell-file"
	kindArtifact       = "artifact"
	kindCIJob          = "ci-job"
)

var kindOrder = []string{
	kindCommand, kindNativeCall, kindEvent, kindFrontendNative, kindFrontendEvent,
	kindCSSMarker, kindPersistence, kindShellFile, kindArtifact, kindCIJob,
}

type inventory struct {
	Baseline string  `json:"baseline"`
	Entries  []entry `json:"entries"`
}

func (inv *inventory) add(e entry) { inv.Entries = append(inv.Entries, e) }

func (inv *inventory) count() int { return len(inv.Entries) }

func (inv *inventory) unclassified() []string {
	var out []string
	for _, e := range inv.Entries {
		if e.Class == "" || e.Owner == "" {
			out = append(out, fmt.Sprintf("%s %s (%s)", e.Kind, e.Name, e.Location))
		}
	}
	return out
}

func (inv *inventory) sorted() []entry {
	rank := map[string]int{}
	for i, k := range kindOrder {
		rank[k] = i
	}
	out := append([]entry(nil), inv.Entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Kind] != rank[out[j].Kind] {
			return rank[out[i].Kind] < rank[out[j].Kind]
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Location < out[j].Location
	})
	return out
}

func (inv *inventory) counts() map[string]map[class]int {
	out := map[string]map[class]int{}
	for _, e := range inv.Entries {
		if out[e.Kind] == nil {
			out[e.Kind] = map[class]int{}
		}
		out[e.Kind][e.Class]++
	}
	return out
}
