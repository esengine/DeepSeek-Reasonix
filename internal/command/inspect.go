package command

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CandidateStatus is the diagnostic disposition of one command file.
type CandidateStatus string

const (
	CandidateWinner   CandidateStatus = "winner"
	CandidateShadowed CandidateStatus = "shadowed"
	CandidateError    CandidateStatus = "error"
)

// Candidate is one command file considered during Load, including shadowed
// sources that later directories override.
type Candidate struct {
	Name        string
	Description string
	Path        string
	Root        string
	Status      CandidateStatus
	WinnerPath  string
	Error       string
}

// RootInfo is one scanned commands directory.
type RootInfo struct {
	Dir    string
	Status string // ok | missing | not-directory | unreadable
}

// Inspection is a read-only snapshot matching command.Load override semantics
// (later dir wins) without changing Load itself.
type Inspection struct {
	Roots      []RootInfo
	Candidates []Candidate
	Winners    []Command
}

// Inspect walks dirs in order (same as Load) and records every candidate.
// Missing dirs are listed as missing and produce no warnings.
func Inspect(dirs ...string) Inspection {
	var roots []RootInfo
	// Per-name list of candidates in scan order; last becomes winner.
	byName := map[string][]Candidate{}
	winners := map[string]Command{}

	for _, dir := range dirs {
		root, err := filepath.Abs(dir)
		if err != nil {
			roots = append(roots, RootInfo{Dir: dir, Status: "unreadable"})
			continue
		}
		st := rootStatus(root)
		roots = append(roots, RootInfo{Dir: root, Status: st})
		if st != "ok" {
			continue
		}
		visited := map[string]bool{}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			visited[real] = true
		} else {
			visited[root] = true
		}
		walkCommands(root, root, visited, func(path string) {
			c, perr := parseFile(root, path)
			if perr != nil {
				name := guessName(root, path)
				byName[name] = append(byName[name], Candidate{
					Name: name, Path: path, Root: root,
					Status: CandidateError, Error: perr.Error(),
				})
				return
			}
			byName[c.Name] = append(byName[c.Name], Candidate{
				Name: c.Name, Description: c.Description, Path: path, Root: root,
				Status: CandidateWinner, // provisional; finalized below
			})
			winners[c.Name] = c
		})
	}

	var candidates []Candidate
	for name, list := range byName {
		// Find last non-error candidate as winner; errors stay as errors.
		winIdx := -1
		for i := len(list) - 1; i >= 0; i-- {
			if list[i].Status != CandidateError {
				winIdx = i
				break
			}
		}
		for i, c := range list {
			if c.Status == CandidateError {
				candidates = append(candidates, c)
				continue
			}
			if i == winIdx {
				c.Status = CandidateWinner
				c.WinnerPath = ""
			} else if winIdx >= 0 {
				c.Status = CandidateShadowed
				c.WinnerPath = list[winIdx].Path
			}
			candidates = append(candidates, c)
		}
		_ = name
	}

	cmds := make([]Command, 0, len(winners))
	for _, c := range winners {
		cmds = append(cmds, c)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		order := map[CandidateStatus]int{CandidateWinner: 0, CandidateShadowed: 1, CandidateError: 2}
		return order[candidates[i].Status] < order[candidates[j].Status]
	})

	return Inspection{Roots: roots, Candidates: candidates, Winners: cmds}
}

func rootStatus(dir string) string {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "unreadable"
	}
	if !info.IsDir() {
		return "not-directory"
	}
	f, err := os.Open(dir)
	if err != nil {
		return "unreadable"
	}
	_ = f.Close()
	return "ok"
}

func guessName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), ".md"), "/", ":")
}
