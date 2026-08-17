package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/fileutil"
)

const (
	desktopProjectOrganizationFile    = "desktop-project-tree-organization.json"
	desktopProjectOrganizationVersion = 1
)

var desktopProjectOrganizationFileMu sync.Mutex

// Project-tree organization lives in a sidecar older Reasonix builds do not
// know about and therefore cannot rewrite. desktop-projects.json retains the
// inline fields for one-release interoperability, while this file is the
// durable source when a user temporarily downgrades and the old build saves.
type desktopProjectOrganization struct {
	Root             string         `json:"root,omitempty"`
	ManualTopicOrder bool           `json:"manualTopicOrder,omitempty"`
	TopicOrder       []string       `json:"topicOrder,omitempty"`
	Groups           []desktopGroup `json:"groups"`
	GroupsRevision   uint64         `json:"groupsRevision,omitempty"`
}

type desktopProjectOrganizationFileData struct {
	Version  int                          `json:"version"`
	Global   desktopProjectOrganization   `json:"global"`
	Projects []desktopProjectOrganization `json:"projects"`
}

func projectsFileHasOrganization(f desktopProjectFile) bool {
	if f.GlobalManualTopicOrder || len(f.GlobalGroups) > 0 || f.GlobalGroupsRevision > 0 {
		return true
	}
	for _, project := range f.Projects {
		if project.ManualTopicOrder || len(project.Groups) > 0 || project.GroupsRevision > 0 {
			return true
		}
	}
	return false
}

func organizationFromProjectsFile(f desktopProjectFile) desktopProjectOrganizationFileData {
	out := desktopProjectOrganizationFileData{
		Version: desktopProjectOrganizationVersion,
		Global: desktopProjectOrganization{
			ManualTopicOrder: f.GlobalManualTopicOrder,
			TopicOrder:       append([]string(nil), f.GlobalTopics...),
			Groups:           normalizeGroups(f.GlobalGroups),
			GroupsRevision:   f.GlobalGroupsRevision,
		},
		Projects: make([]desktopProjectOrganization, 0, len(f.Projects)),
	}
	for _, project := range f.Projects {
		out.Projects = append(out.Projects, desktopProjectOrganization{
			Root:             project.Root,
			ManualTopicOrder: project.ManualTopicOrder,
			TopicOrder:       append([]string(nil), project.Topics...),
			Groups:           normalizeGroups(project.Groups),
			GroupsRevision:   project.GroupsRevision,
		})
	}
	return out
}

func applyPersistedTopicOrder(current, persisted []string) []string {
	available := make(map[string]bool, len(current))
	for _, topicID := range current {
		available[topicID] = true
	}
	seen := make(map[string]bool, len(current))
	out := make([]string, 0, len(current))
	for _, topicID := range persisted {
		topicID = strings.TrimSpace(topicID)
		if topicID == "" || !available[topicID] || seen[topicID] {
			continue
		}
		seen[topicID] = true
		out = append(out, topicID)
	}
	for _, topicID := range current {
		if !seen[topicID] {
			seen[topicID] = true
			out = append(out, topicID)
		}
	}
	return out
}

func groupsWithoutDeletedTopics(groups []desktopGroup, deleted map[string]bool) []desktopGroup {
	out := normalizeGroups(groups)
	for index := range out {
		kept := out[index].TopicIDs[:0]
		for _, topicID := range out[index].TopicIDs {
			if !deleted[topicID] {
				kept = append(kept, topicID)
			}
		}
		out[index].TopicIDs = kept
	}
	return out
}

func applyProjectOrganization(f desktopProjectFile, organization desktopProjectOrganizationFileData) desktopProjectFile {
	deleted := make(map[string]bool, len(f.DeletedTopics))
	for _, topicID := range f.DeletedTopics {
		deleted[topicID] = true
	}
	f.GlobalManualTopicOrder = organization.Global.ManualTopicOrder
	if f.GlobalManualTopicOrder {
		f.GlobalTopics = applyPersistedTopicOrder(f.GlobalTopics, organization.Global.TopicOrder)
	}
	f.GlobalGroups = groupsWithoutDeletedTopics(organization.Global.Groups, deleted)
	f.GlobalGroupsRevision = organization.Global.GroupsRevision
	for _, persisted := range organization.Projects {
		index := projectIndexByRoot(f.Projects, persisted.Root)
		if index < 0 {
			continue
		}
		project := &f.Projects[index]
		project.ManualTopicOrder = persisted.ManualTopicOrder
		if project.ManualTopicOrder {
			project.Topics = applyPersistedTopicOrder(project.Topics, persisted.TopicOrder)
		}
		project.Groups = groupsWithoutDeletedTopics(persisted.Groups, deleted)
		project.GroupsRevision = persisted.GroupsRevision
	}
	return normalizeProjectsFile(f)
}

func loadProjectOrganizationFile() (desktopProjectOrganizationFileData, bool) {
	path := filepath.Join(desktopConfigDir(), desktopProjectOrganizationFile)
	b, err := readFileUTF8(path)
	if err != nil {
		return desktopProjectOrganizationFileData{}, false
	}
	var organization desktopProjectOrganizationFileData
	if json.Unmarshal(b, &organization) != nil || organization.Version != desktopProjectOrganizationVersion {
		return desktopProjectOrganizationFileData{}, false
	}
	if organization.Global.Groups == nil {
		organization.Global.Groups = []desktopGroup{}
	}
	return organization, true
}

func saveProjectOrganizationFile(f desktopProjectFile) error {
	desktopProjectOrganizationFileMu.Lock()
	defer desktopProjectOrganizationFileMu.Unlock()

	dir := desktopConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, desktopProjectOrganizationFile)
	if existing, err := readFileUTF8(path); err == nil {
		var header struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(existing, &header) == nil && header.Version > desktopProjectOrganizationVersion {
			return fmt.Errorf("project organization version %d is newer than supported version %d", header.Version, desktopProjectOrganizationVersion)
		}
	}
	b, err := json.MarshalIndent(organizationFromProjectsFile(f), "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}
