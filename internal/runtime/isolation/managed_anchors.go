package isolation

import (
	"os"
	"path/filepath"
)

type candidateAnchors struct {
	Source    string `json:"source"`
	Workspace string `json:"workspace"`
	Common    string `json:"common"`
	Slot      string `json:"slot,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Admin     string `json:"admin,omitempty"`
}

func directoryID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return candidateDirectoryID(f)
}

func captureCandidateAnchors(d durableEntry, materialized bool) (candidateAnchors, error) {
	var anchors candidateAnchors
	paths := []struct {
		path   string
		target *string
	}{
		{d.Snap.RepoRoot, &anchors.Source}, {candidateWorkspace(d), &anchors.Workspace}, {d.Snap.Repo.CommonDir, &anchors.Common},
	}
	if materialized {
		paths = append(paths, struct {
			path   string
			target *string
		}{filepath.Dir(d.Cand.WorktreeRoot), &anchors.Slot}, struct {
			path   string
			target *string
		}{d.Cand.WorktreeRoot, &anchors.Candidate}, struct {
			path   string
			target *string
		}{d.Cand.Repo.GitDir, &anchors.Admin})
	}
	for _, item := range paths {
		id, err := directoryID(item.path)
		if err != nil {
			return anchors, err
		}
		*item.target = id
	}
	return anchors, nil
}

func (a candidateAnchors) sourceMatches(d durableEntry) bool {
	current, err := captureCandidateAnchors(d, false)
	return err == nil && a.Source != "" && a.Source == current.Source && a.Workspace == current.Workspace && a.Common == current.Common
}

func directoryMatches(path, id string) bool {
	current, err := directoryID(path)
	return err == nil && id != "" && id == current
}
